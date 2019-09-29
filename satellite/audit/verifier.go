// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package audit

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"time"

	"github.com/vivint/infectious"
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"gopkg.in/spacemonkeygo/monkit.v2"

	"storj.io/storj/internal/errs2"
	"storj.io/storj/internal/memory"
	"storj.io/storj/pkg/identity"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/pkcrypto"
	"storj.io/storj/pkg/rpc"
	"storj.io/storj/pkg/rpc/rpcstatus"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/satellite/metainfo"
	"storj.io/storj/satellite/orders"
	"storj.io/storj/satellite/overlay"
	"storj.io/storj/storage"
	"storj.io/storj/uplink/eestream"
	"storj.io/storj/uplink/piecestore"
)

var (
	mon = monkit.Package()

	// ErrNotEnoughShares is the errs class for when not enough shares are available to do an audit
	ErrNotEnoughShares = errs.Class("not enough shares for successful audit")
	// ErrSegmentDeleted is the errs class when the audited segment was deleted during the audit
	ErrSegmentDeleted = errs.Class("segment deleted during audit")
)

// Share represents required information about an audited share
type Share struct {
	Error    error
	PieceNum int
	NodeID   storj.NodeID
	Data     []byte
}

// Verifier helps verify the correctness of a given stripe
//
// architecture: Worker
type Verifier struct {
	log                *zap.Logger
	metainfo           *metainfo.Service
	orders             *orders.Service
	auditor            *identity.PeerIdentity
	dialer             rpc.Dialer
	overlay            *overlay.Service
	containment        Containment
	minBytesPerSecond  memory.Size
	minDownloadTimeout time.Duration

	OnTestingCheckSegmentAlteredHook func()
}

// NewVerifier creates a Verifier
func NewVerifier(log *zap.Logger, metainfo *metainfo.Service, dialer rpc.Dialer, overlay *overlay.Service, containment Containment, orders *orders.Service, id *identity.FullIdentity, minBytesPerSecond memory.Size, minDownloadTimeout time.Duration) *Verifier {
	return &Verifier{
		log:                log,
		metainfo:           metainfo,
		orders:             orders,
		auditor:            id.PeerIdentity(),
		dialer:             dialer,
		overlay:            overlay,
		containment:        containment,
		minBytesPerSecond:  minBytesPerSecond,
		minDownloadTimeout: minDownloadTimeout,
	}
}

// Verify downloads shares then verifies the data correctness at a random stripe.
func (verifier *Verifier) Verify(ctx context.Context, path storj.Path, skip map[storj.NodeID]bool) (report *Report, err error) {
	defer mon.Task()(&ctx)(&err)

	pointer, err := verifier.metainfo.Get(ctx, path)
	if err != nil {
		if storage.ErrKeyNotFound.Has(err) {
			return nil, ErrSegmentDeleted.New("%q", path)
		}
		return nil, err
	}

	randomIndex, err := GetRandomStripe(ctx, pointer)
	if err != nil {
		return nil, err
	}

	shareSize := pointer.GetRemote().GetRedundancy().GetErasureShareSize()
	bucketID := createBucketID(path)

	var offlineNodes storj.NodeIDList
	var failedNodes storj.NodeIDList
	containedNodes := make(map[int]storj.NodeID)
	sharesToAudit := make(map[int]Share)

	orderLimits, privateKey, err := verifier.orders.CreateAuditOrderLimits(ctx, bucketID, pointer, skip)
	if err != nil {
		return nil, err
	}

	// NOTE offlineNodes will include disqualified nodes because they aren't in
	// the skip list
	offlineNodes = getOfflineNodes(pointer, orderLimits, skip)
	if len(offlineNodes) > 0 {
		verifier.log.Debug("Verify: order limits not created for some nodes (offline/disqualified)", zap.String("Segment Path", path), zap.Strings("Node IDs", offlineNodes.Strings()))
	}

	shares, err := verifier.DownloadShares(ctx, orderLimits, privateKey, randomIndex, shareSize)
	if err != nil {
		return &Report{
			Offlines: offlineNodes,
		}, err
	}

	_, err = verifier.checkIfSegmentAltered(ctx, path, pointer)
	if err != nil {
		return &Report{
			Offlines: offlineNodes,
		}, err
	}

	for pieceNum, share := range shares {
		if share.Error == nil {
			// no error -- share downloaded successfully
			sharesToAudit[pieceNum] = share
			continue
		}
		if rpc.Error.Has(share.Error) {
			if errs.Is(share.Error, context.DeadlineExceeded) {
				// dial timeout
				offlineNodes = append(offlineNodes, share.NodeID)
				verifier.log.Debug("Verify: dial timeout (offline)", zap.String("Segment Path", path), zap.Stringer("Node ID", share.NodeID), zap.Error(share.Error))
				continue
			}
			if errs2.IsRPC(share.Error, rpcstatus.Unknown) {
				// dial failed -- offline node
				offlineNodes = append(offlineNodes, share.NodeID)
				verifier.log.Debug("Verify: dial failed (offline)", zap.String("Segment Path", path), zap.Stringer("Node ID", share.NodeID), zap.Error(share.Error))
				continue
			}
			// unknown transport error
			containedNodes[pieceNum] = share.NodeID
			verifier.log.Debug("Verify: unknown transport error (contained)", zap.String("Segment Path", path), zap.Stringer("Node ID", share.NodeID), zap.Error(share.Error))
		}

		if errs2.IsRPC(share.Error, rpcstatus.NotFound) {
			// missing share
			failedNodes = append(failedNodes, share.NodeID)
			verifier.log.Debug("Verify: piece not found (audit failed)", zap.String("Segment Path", path), zap.Stringer("Node ID", share.NodeID), zap.Error(share.Error))
			continue
		}

		if errs2.IsRPC(share.Error, rpcstatus.DeadlineExceeded) {
			// dial successful, but download timed out
			containedNodes[pieceNum] = share.NodeID
			verifier.log.Debug("Verify: download timeout (contained)", zap.String("Segment Path", path), zap.Stringer("Node ID", share.NodeID), zap.Error(share.Error))
			continue
		}

		// unknown error
		containedNodes[pieceNum] = share.NodeID
		verifier.log.Debug("Verify: unknown error (contained)", zap.String("Segment Path", path), zap.Stringer("Node ID", share.NodeID), zap.Error(share.Error))
	}

	required := int(pointer.Remote.Redundancy.GetMinReq())
	total := int(pointer.Remote.Redundancy.GetTotal())

	if len(sharesToAudit) < required {
		return &Report{
			Fails:    failedNodes,
			Offlines: offlineNodes,
		}, ErrNotEnoughShares.New("got %d, required %d", len(sharesToAudit), required)
	}

	pieceNums, correctedShares, err := auditShares(ctx, required, total, sharesToAudit)
	if err != nil {
		return &Report{
			Fails:    failedNodes,
			Offlines: offlineNodes,
		}, err
	}

	for _, pieceNum := range pieceNums {
		failedNodes = append(failedNodes, shares[pieceNum].NodeID)
	}
	// remove failed audit pieces from the pointer so as to only penalize once for failed audits
	err = verifier.removeFailedPieces(ctx, path, pointer, failedNodes)
	if err != nil {
		verifier.log.Warn("Verify: failed to delete failed pieces", zap.String("Segment Path", path), zap.Error(err))
	}

	successNodes := getSuccessNodes(ctx, shares, failedNodes, offlineNodes, containedNodes)

	totalInPointer := len(pointer.GetRemote().GetRemotePieces())
	numOffline := len(offlineNodes)
	numSuccessful := len(successNodes)
	numFailed := len(failedNodes)
	numContained := len(containedNodes)
	totalAudited := numSuccessful + numFailed + numOffline + numContained
	auditedPercentage := float64(totalAudited) / float64(totalInPointer)
	offlinePercentage := float64(0)
	successfulPercentage := float64(0)
	failedPercentage := float64(0)
	containedPercentage := float64(0)
	if totalAudited > 0 {
		offlinePercentage = float64(numOffline) / float64(totalAudited)
		successfulPercentage = float64(numSuccessful) / float64(totalAudited)
		failedPercentage = float64(numFailed) / float64(totalAudited)
		containedPercentage = float64(numContained) / float64(totalAudited)
	}

	mon.Meter("audit_success_nodes_global").Mark(numSuccessful)
	mon.Meter("audit_fail_nodes_global").Mark(numFailed)
	mon.Meter("audit_offline_nodes_global").Mark(numOffline)
	mon.Meter("audit_contained_nodes_global").Mark(numContained)
	mon.Meter("audit_total_nodes_global").Mark(totalAudited)
	mon.Meter("audit_total_pointer_nodes_global").Mark(totalInPointer)

	mon.IntVal("audit_success_nodes").Observe(int64(numSuccessful))
	mon.IntVal("audit_fail_nodes").Observe(int64(numFailed))
	mon.IntVal("audit_offline_nodes").Observe(int64(numOffline))
	mon.IntVal("audit_contained_nodes").Observe(int64(numContained))
	mon.IntVal("audit_total_nodes").Observe(int64(totalAudited))
	mon.IntVal("audit_total_pointer_nodes").Observe(int64(totalInPointer))
	mon.FloatVal("audited_percentage").Observe(auditedPercentage)
	mon.FloatVal("audit_offline_percentage").Observe(offlinePercentage)
	mon.FloatVal("audit_successful_percentage").Observe(successfulPercentage)
	mon.FloatVal("audit_failed_percentage").Observe(failedPercentage)
	mon.FloatVal("audit_contained_percentage").Observe(containedPercentage)

	pendingAudits, err := createPendingAudits(ctx, containedNodes, correctedShares, pointer, randomIndex, path)
	if err != nil {
		return &Report{
			Successes: successNodes,
			Fails:     failedNodes,
			Offlines:  offlineNodes,
		}, err
	}

	return &Report{
		Successes:     successNodes,
		Fails:         failedNodes,
		Offlines:      offlineNodes,
		PendingAudits: pendingAudits,
	}, nil
}

// DownloadShares downloads shares from the nodes where remote pieces are located
func (verifier *Verifier) DownloadShares(ctx context.Context, limits []*pb.AddressedOrderLimit, piecePrivateKey storj.PiecePrivateKey, stripeIndex int64, shareSize int32) (shares map[int]Share, err error) {
	defer mon.Task()(&ctx)(&err)

	shares = make(map[int]Share, len(limits))
	ch := make(chan *Share, len(limits))

	for i, limit := range limits {
		if limit == nil {
			ch <- nil
			continue
		}

		go func(i int, limit *pb.AddressedOrderLimit) {
			share, err := verifier.GetShare(ctx, limit, piecePrivateKey, stripeIndex, shareSize, i)
			if err != nil {
				share = Share{
					Error:    err,
					PieceNum: i,
					NodeID:   limit.GetLimit().StorageNodeId,
					Data:     nil,
				}
			}
			ch <- &share
		}(i, limit)
	}

	for range limits {
		share := <-ch
		if share != nil {
			shares[share.PieceNum] = *share
		}
	}

	return shares, nil
}

// Reverify reverifies the contained nodes in the stripe
func (verifier *Verifier) Reverify(ctx context.Context, path storj.Path) (report *Report, err error) {
	defer mon.Task()(&ctx)(&err)

	// result status enum
	const (
		skipped = iota
		success
		offline
		failed
		contained
		erred
	)

	type result struct {
		nodeID       storj.NodeID
		status       int
		pendingAudit *PendingAudit
		err          error
	}

	pointer, err := verifier.metainfo.Get(ctx, path)
	if err != nil {
		if storage.ErrKeyNotFound.Has(err) {
			return nil, ErrSegmentDeleted.New("%q", path)
		}
		return nil, err
	}

	pieces := pointer.GetRemote().GetRemotePieces()
	ch := make(chan result, len(pieces))
	var containedInSegment int64

	for _, piece := range pieces {
		pending, err := verifier.containment.Get(ctx, piece.NodeId)
		if err != nil {
			if ErrContainedNotFound.Has(err) {
				ch <- result{nodeID: piece.NodeId, status: skipped}
				continue
			}
			ch <- result{nodeID: piece.NodeId, status: erred, err: err}
			verifier.log.Debug("Reverify: error getting from containment db", zap.String("Segment Path", path), zap.Stringer("Node ID", piece.NodeId), zap.Error(err))
			continue
		}
		containedInSegment++

		go func(pending *PendingAudit) {
			pendingPointer, err := verifier.metainfo.Get(ctx, pending.Path)
			if err != nil {
				if storage.ErrKeyNotFound.Has(err) {
					// segment has been deleted since node was contained
					_, errDelete := verifier.containment.Delete(ctx, pending.NodeID)
					if errDelete != nil {
						verifier.log.Debug("Error deleting node from containment db", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(errDelete))
					}
					ch <- result{nodeID: pending.NodeID, status: skipped}
					return
				}

				ch <- result{nodeID: pending.NodeID, status: erred, err: err}
				verifier.log.Debug("Reverify: error getting pending pointer from metainfo", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
				return
			}
			if pendingPointer.GetRemote().RootPieceId != pending.PieceID {
				// segment has changed since initial containment
				_, errDelete := verifier.containment.Delete(ctx, pending.NodeID)
				if errDelete != nil {
					verifier.log.Debug("Error deleting node from containment db", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(errDelete))
				}
				ch <- result{nodeID: pending.NodeID, status: skipped}
				return
			}
			var pieceNum int32
			found := false
			for _, piece := range pendingPointer.GetRemote().GetRemotePieces() {
				if piece.NodeId == pending.NodeID {
					pieceNum = piece.PieceNum
					found = true
				}
			}
			if !found {
				// node is no longer in pointer, so remove from containment
				ch <- result{nodeID: pending.NodeID, status: erred, err: err}
				_, errDelete := verifier.containment.Delete(ctx, pending.NodeID)
				if errDelete != nil {
					verifier.log.Debug("Error deleting node from containment db", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(errDelete))
				}
				ch <- result{nodeID: pending.NodeID, status: skipped}
				return
			}

			limit, piecePrivateKey, err := verifier.orders.CreateAuditOrderLimit(ctx, createBucketID(pending.Path), pending.NodeID, pieceNum, pending.PieceID, pending.ShareSize)
			if err != nil {
				if overlay.ErrNodeDisqualified.Has(err) {
					_, errDelete := verifier.containment.Delete(ctx, pending.NodeID)
					if errDelete != nil {
						verifier.log.Debug("Error deleting disqualified node from containment db", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(errDelete))
					}
					ch <- result{nodeID: pending.NodeID, status: erred, err: err}
					verifier.log.Debug("Reverify: order limit not created (disqualified)", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID))
					return
				}
				if overlay.ErrNodeOffline.Has(err) {
					ch <- result{nodeID: pending.NodeID, status: offline}
					verifier.log.Debug("Reverify: order limit not created (offline)", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID))
					return
				}
				ch <- result{nodeID: pending.NodeID, status: erred, err: err}
				verifier.log.Debug("Reverify: error creating order limit", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
				return
			}

			share, err := verifier.GetShare(ctx, limit, piecePrivateKey, pending.StripeIndex, pending.ShareSize, int(pieceNum))

			// check if the pending audit was deleted while downloading the share
			_, getErr := verifier.containment.Get(ctx, pending.NodeID)
			if getErr != nil {
				if ErrContainedNotFound.Has(getErr) {
					ch <- result{nodeID: pending.NodeID, status: skipped}
					verifier.log.Debug("Reverify: pending audit deleted during reverification", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(getErr))
					return
				}
				ch <- result{nodeID: pending.NodeID, status: erred, err: getErr}
				verifier.log.Debug("Reverify: error getting from containment db", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(getErr))
				return
			}

			// analyze the error from GetShare
			if err != nil {
				if rpc.Error.Has(err) {
					if errs.Is(err, context.DeadlineExceeded) {
						// dial timeout
						ch <- result{nodeID: pending.NodeID, status: offline}
						verifier.log.Debug("Reverify: dial timeout (offline)", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
						return
					}
					if errs2.IsRPC(err, rpcstatus.Unknown) {
						// dial failed -- offline node
						verifier.log.Debug("Reverify: dial failed (offline)", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
						ch <- result{nodeID: pending.NodeID, status: offline}
						return
					}
					// unknown transport error
					ch <- result{nodeID: pending.NodeID, status: contained, pendingAudit: pending}
					verifier.log.Debug("Reverify: unknown transport error (contained)", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
					return
				}
				if errs2.IsRPC(err, rpcstatus.NotFound) {
					// Get the original segment pointer in the metainfo
					oldPtr, err := verifier.checkIfSegmentAltered(ctx, pending.Path, pendingPointer)
					if err != nil {
						ch <- result{nodeID: pending.NodeID, status: success}
						verifier.log.Debug("Reverify: audit source deleted before reverification", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
						return
					}
					// remove failed audit pieces from the pointer so as to only penalize once for failed audits
					err = verifier.removeFailedPieces(ctx, pending.Path, oldPtr, storj.NodeIDList{pending.NodeID})
					if err != nil {
						verifier.log.Warn("Reverify: failed to delete failed pieces", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
					}
					// missing share
					ch <- result{nodeID: pending.NodeID, status: failed}
					verifier.log.Debug("Reverify: piece not found (audit failed)", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
					return
				}
				if errs2.IsRPC(err, rpcstatus.DeadlineExceeded) {
					// dial successful, but download timed out
					ch <- result{nodeID: pending.NodeID, status: contained, pendingAudit: pending}
					verifier.log.Debug("Reverify: download timeout (contained)", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
					return
				}
				// unknown error
				ch <- result{nodeID: pending.NodeID, status: contained, pendingAudit: pending}
				verifier.log.Debug("Reverify: unknown error (contained)", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
				return
			}
			downloadedHash := pkcrypto.SHA256Hash(share.Data)
			if bytes.Equal(downloadedHash, pending.ExpectedShareHash) {
				ch <- result{nodeID: pending.NodeID, status: success}
				verifier.log.Debug("Reverify: hashes match (audit success)", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID))
			} else {
				oldPtr, err := verifier.checkIfSegmentAltered(ctx, pending.Path, pendingPointer)
				if err != nil {
					ch <- result{nodeID: pending.NodeID, status: success}
					verifier.log.Debug("Reverify: audit source deleted before reverification", zap.String("Segment Path", path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
					return
				}
				// remove failed audit pieces from the pointer so as to only penalize once for failed audits
				err = verifier.removeFailedPieces(ctx, pending.Path, oldPtr, storj.NodeIDList{pending.NodeID})
				if err != nil {
					verifier.log.Warn("Reverify: failed to delete failed pieces", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID), zap.Error(err))
				}
				verifier.log.Debug("Reverify: hashes mismatch (audit failed)", zap.String("Segment Path", pending.Path), zap.Stringer("Node ID", pending.NodeID),
					zap.Binary("expected hash", pending.ExpectedShareHash), zap.Binary("downloaded hash", downloadedHash))
				ch <- result{nodeID: pending.NodeID, status: failed}
			}
		}(pending)
	}

	report = &Report{}
	for range pieces {
		result := <-ch
		switch result.status {
		case success:
			report.Successes = append(report.Successes, result.nodeID)
		case offline:
			report.Offlines = append(report.Offlines, result.nodeID)
		case failed:
			report.Fails = append(report.Fails, result.nodeID)
		case contained:
			report.PendingAudits = append(report.PendingAudits, result.pendingAudit)
		case erred:
			err = errs.Combine(err, result.err)
		}
	}

	mon.Meter("reverify_successes_global").Mark(len(report.Successes))
	mon.Meter("reverify_offlines_global").Mark(len(report.Offlines))
	mon.Meter("reverify_fails_global").Mark(len(report.Fails))
	mon.Meter("reverify_contained_global").Mark(len(report.PendingAudits))

	mon.IntVal("reverify_successes").Observe(int64(len(report.Successes)))
	mon.IntVal("reverify_offlines").Observe(int64(len(report.Offlines)))
	mon.IntVal("reverify_fails").Observe(int64(len(report.Fails)))
	mon.IntVal("reverify_contained").Observe(int64(len(report.PendingAudits)))

	mon.IntVal("reverify_contained_in_segment").Observe(containedInSegment)
	mon.IntVal("reverify_total_in_segment").Observe(int64(len(pieces)))

	return report, err
}

// GetShare use piece store client to download shares from nodes
func (verifier *Verifier) GetShare(ctx context.Context, limit *pb.AddressedOrderLimit, piecePrivateKey storj.PiecePrivateKey, stripeIndex int64, shareSize int32, pieceNum int) (share Share, err error) {
	defer mon.Task()(&ctx)(&err)

	bandwidthMsgSize := shareSize

	// determines number of seconds allotted for receiving data from a storage node
	timedCtx := ctx
	if verifier.minBytesPerSecond > 0 {
		maxTransferTime := time.Duration(int64(time.Second) * int64(bandwidthMsgSize) / verifier.minBytesPerSecond.Int64())
		if maxTransferTime < verifier.minDownloadTimeout {
			maxTransferTime = verifier.minDownloadTimeout
		}
		var cancel func()
		timedCtx, cancel = context.WithTimeout(ctx, maxTransferTime)
		defer cancel()
	}

	storageNodeID := limit.GetLimit().StorageNodeId
	log := verifier.log.Named(storageNodeID.String())
	target := &pb.Node{Id: storageNodeID, Address: limit.GetStorageNodeAddress()}

	ps, err := piecestore.Dial(timedCtx, verifier.dialer, target, log, piecestore.DefaultConfig)
	if err != nil {
		return Share{}, Error.Wrap(err)
	}
	defer func() {
		err := ps.Close()
		if err != nil {
			verifier.log.Error("audit verifier failed to close conn to node: %+v", zap.Error(err))
		}
	}()

	offset := int64(shareSize) * stripeIndex

	downloader, err := ps.Download(timedCtx, limit.GetLimit(), piecePrivateKey, offset, int64(shareSize))
	if err != nil {
		return Share{}, err
	}
	defer func() { err = errs.Combine(err, downloader.Close()) }()

	buf := make([]byte, shareSize)
	_, err = io.ReadFull(downloader, buf)
	if err != nil {
		return Share{}, err
	}

	return Share{
		Error:    nil,
		PieceNum: pieceNum,
		NodeID:   storageNodeID,
		Data:     buf,
	}, nil
}

// removeFailedPieces removes lost pieces from a pointer
func (verifier *Verifier) removeFailedPieces(ctx context.Context, path string, pointer *pb.Pointer, failedNodes storj.NodeIDList) (err error) {
	defer mon.Task()(&ctx)(&err)
	if len(failedNodes) == 0 {
		return nil
	}

	var toRemove []*pb.RemotePiece
OUTER:
	for _, piece := range pointer.GetRemote().GetRemotePieces() {
		for _, failedNode := range failedNodes {
			if piece.NodeId == failedNode {
				toRemove = append(toRemove, piece)
				continue OUTER
			}
		}
	}

	// Update the segment pointer in the metainfo
	_, err = verifier.metainfo.UpdatePieces(ctx, path, pointer, nil, toRemove)
	return err
}

// checkIfSegmentAltered checks if path's pointer has been altered since path was selected.
func (verifier *Verifier) checkIfSegmentAltered(ctx context.Context, segmentPath string, oldPointer *pb.Pointer) (newPointer *pb.Pointer, err error) {
	defer mon.Task()(&ctx)(&err)

	if verifier.OnTestingCheckSegmentAlteredHook != nil {
		verifier.OnTestingCheckSegmentAlteredHook()
	}

	newPointer, err = verifier.metainfo.Get(ctx, segmentPath)
	if err != nil {
		if storage.ErrKeyNotFound.Has(err) {
			return nil, ErrSegmentDeleted.New("%q", segmentPath)
		}
		return nil, err
	}

	if oldPointer != nil && oldPointer.CreationDate != newPointer.CreationDate {
		return nil, ErrSegmentDeleted.New("%q", segmentPath)
	}
	return newPointer, nil
}

// auditShares takes the downloaded shares and uses infectious's Correct function to check that they
// haven't been altered. auditShares returns a slice containing the piece numbers of altered shares,
// and a slice of the corrected shares.
func auditShares(ctx context.Context, required, total int, originals map[int]Share) (pieceNums []int, corrected []infectious.Share, err error) {
	defer mon.Task()(&ctx)(&err)
	f, err := infectious.NewFEC(required, total)
	if err != nil {
		return nil, nil, err
	}

	copies, err := makeCopies(ctx, originals)
	if err != nil {
		return nil, nil, err
	}

	err = f.Correct(copies)
	if err != nil {
		return nil, nil, err
	}

	for _, share := range copies {
		if !bytes.Equal(originals[share.Number].Data, share.Data) {
			pieceNums = append(pieceNums, share.Number)
		}
	}
	return pieceNums, copies, nil
}

// makeCopies takes in a map of audit Shares and deep copies their data to a slice of infectious Shares
func makeCopies(ctx context.Context, originals map[int]Share) (copies []infectious.Share, err error) {
	defer mon.Task()(&ctx)(&err)
	copies = make([]infectious.Share, 0, len(originals))
	for _, original := range originals {
		copies = append(copies, infectious.Share{
			Data:   append([]byte{}, original.Data...),
			Number: original.PieceNum})
	}
	return copies, nil
}

// getOfflines nodes returns these storage nodes from pointer which have no
// order limit nor are skipped.
func getOfflineNodes(pointer *pb.Pointer, limits []*pb.AddressedOrderLimit, skip map[storj.NodeID]bool) storj.NodeIDList {
	var offlines storj.NodeIDList

	nodesWithLimit := make(map[storj.NodeID]bool, len(limits))
	for _, limit := range limits {
		if limit != nil {
			nodesWithLimit[limit.GetLimit().StorageNodeId] = true
		}
	}

	for _, piece := range pointer.GetRemote().GetRemotePieces() {
		if !nodesWithLimit[piece.NodeId] && !skip[piece.NodeId] {
			offlines = append(offlines, piece.NodeId)
		}
	}

	return offlines
}

// getSuccessNodes uses the failed nodes, offline nodes and contained nodes arrays to determine which nodes passed the audit
func getSuccessNodes(ctx context.Context, shares map[int]Share, failedNodes, offlineNodes storj.NodeIDList, containedNodes map[int]storj.NodeID) (successNodes storj.NodeIDList) {
	defer mon.Task()(&ctx)(nil)
	fails := make(map[storj.NodeID]bool)
	for _, fail := range failedNodes {
		fails[fail] = true
	}
	for _, offline := range offlineNodes {
		fails[offline] = true
	}
	for _, contained := range containedNodes {
		fails[contained] = true
	}

	for _, share := range shares {
		if !fails[share.NodeID] {
			successNodes = append(successNodes, share.NodeID)
		}
	}

	return successNodes
}

func createBucketID(path storj.Path) []byte {
	comps := storj.SplitPath(path)
	if len(comps) < 3 {
		return nil
	}
	// project_id/bucket_name
	return []byte(storj.JoinPaths(comps[0], comps[2]))
}

func createPendingAudits(ctx context.Context, containedNodes map[int]storj.NodeID, correctedShares []infectious.Share, pointer *pb.Pointer, randomIndex int64, path storj.Path) (pending []*PendingAudit, err error) {
	defer mon.Task()(&ctx)(&err)

	if len(containedNodes) == 0 {
		return nil, nil
	}

	redundancy := pointer.GetRemote().GetRedundancy()
	required := int(redundancy.GetMinReq())
	total := int(redundancy.GetTotal())
	shareSize := redundancy.GetErasureShareSize()

	fec, err := infectious.NewFEC(required, total)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	stripeData, err := rebuildStripe(ctx, fec, correctedShares, int(shareSize))
	if err != nil {
		return nil, Error.Wrap(err)
	}

	for pieceNum, nodeID := range containedNodes {
		share := make([]byte, shareSize)
		err = fec.EncodeSingle(stripeData, share, pieceNum)
		if err != nil {
			return nil, Error.Wrap(err)
		}
		pending = append(pending, &PendingAudit{
			NodeID:            nodeID,
			PieceID:           pointer.GetRemote().RootPieceId,
			StripeIndex:       randomIndex,
			ShareSize:         shareSize,
			ExpectedShareHash: pkcrypto.SHA256Hash(share),
			Path:              path,
		})
	}

	return pending, nil
}

func rebuildStripe(ctx context.Context, fec *infectious.FEC, corrected []infectious.Share, shareSize int) (_ []byte, err error) {
	defer mon.Task()(&ctx)(&err)
	stripe := make([]byte, fec.Required()*shareSize)
	err = fec.Rebuild(corrected, func(share infectious.Share) {
		copy(stripe[share.Number*shareSize:], share.Data)
	})
	if err != nil {
		return nil, err
	}
	return stripe, nil
}

// GetRandomStripe takes a pointer and returns a random stripe index within that pointer.
func GetRandomStripe(ctx context.Context, pointer *pb.Pointer) (index int64, err error) {
	defer mon.Task()(&ctx)(&err)
	redundancy, err := eestream.NewRedundancyStrategyFromProto(pointer.GetRemote().GetRedundancy())
	if err != nil {
		return 0, err
	}

	// the last segment could be smaller than stripe size
	if pointer.GetSegmentSize() < int64(redundancy.StripeSize()) {
		return 0, nil
	}

	var src cryptoSource
	rnd := rand.New(src)
	numStripes := pointer.GetSegmentSize() / int64(redundancy.StripeSize())
	randomStripeIndex := rnd.Int63n(numStripes)

	return randomStripeIndex, nil
}
