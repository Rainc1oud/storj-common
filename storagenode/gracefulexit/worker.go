// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package gracefulexit

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/rpc"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/storagenode/pieces"
	"storj.io/storj/storagenode/piecestore"
	"storj.io/storj/storagenode/satellites"
	"storj.io/storj/uplink/ecclient"
)

// Worker is responsible for completing the graceful exit for a given satellite.
type Worker struct {
	log           *zap.Logger
	store         *pieces.Store
	satelliteDB   satellites.DB
	dialer        rpc.Dialer
	satelliteID   storj.NodeID
	satelliteAddr string
	ecclient      ecclient.Client
}

// NewWorker instantiates Worker.
func NewWorker(log *zap.Logger, store *pieces.Store, satelliteDB satellites.DB, dialer rpc.Dialer, satelliteID storj.NodeID, satelliteAddr string) *Worker {
	return &Worker{
		log:           log,
		store:         store,
		satelliteDB:   satelliteDB,
		dialer:        dialer,
		satelliteID:   satelliteID,
		satelliteAddr: satelliteAddr,
		ecclient:      ecclient.NewClient(log, dialer, 0),
	}
}

// Run calls the satellite endpoint, transfers pieces, validates, and responds with success or failure.
// It also marks the satellite finished once all the pieces have been transferred
// TODO handle transfers in parallel
func (worker *Worker) Run(ctx context.Context, done func()) (err error) {
	defer mon.Task()(&ctx)(&err)
	defer done()

	worker.log.Debug("running worker")

	conn, err := worker.dialer.DialAddressID(ctx, worker.satelliteAddr, worker.satelliteID)
	if err != nil {
		return errs.Wrap(err)
	}
	defer func() {
		err = errs.Combine(err, conn.Close())
	}()

	client := conn.SatelliteGracefulExitClient()

	c, err := client.Process(ctx)
	if err != nil {
		return errs.Wrap(err)
	}

	for {
		response, err := c.Recv()
		if errs.Is(err, io.EOF) {
			// Done
			break
		}
		if err != nil {
			// TODO what happened
			return errs.Wrap(err)
		}

		switch msg := response.GetMessage().(type) {
		case *pb.SatelliteMessage_NotReady:
			break // wait until next worker execution
		case *pb.SatelliteMessage_TransferPiece:
			pieceID := msg.TransferPiece.OriginalPieceId
			reader, err := worker.store.Reader(ctx, worker.satelliteID, pieceID)
			if err != nil {
				transferErr := pb.TransferFailed_UNKNOWN
				if errs.Is(err, os.ErrNotExist) {
					transferErr = pb.TransferFailed_NOT_FOUND
				}
				worker.log.Error("failed to get piece reader.", zap.Stringer("satellite ID", worker.satelliteID), zap.Stringer("piece ID", pieceID), zap.Error(errs.Wrap(err)))
				worker.handleFailure(ctx, transferErr, pieceID, c.Send)
				continue
			}

			addrLimit := msg.TransferPiece.GetAddressedOrderLimit()
			pk := msg.TransferPiece.PrivateKey

			originalHash, originalOrderLimit, err := worker.getHashAndLimit(ctx, reader, addrLimit.GetLimit())
			if err != nil {
				worker.log.Error("failed to get piece hash and order limit.", zap.Stringer("satellite ID", worker.satelliteID), zap.Stringer("piece ID", pieceID), zap.Error(errs.Wrap(err)))
				worker.handleFailure(ctx, pb.TransferFailed_UNKNOWN, pieceID, c.Send)
				continue
			}

			putCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			// TODO what's the typical expiration setting?
			pieceHash, err := worker.ecclient.PutPiece(putCtx, ctx, addrLimit, pk, reader, time.Now().Add(time.Second*600))
			if err != nil {
				if piecestore.ErrVerifyUntrusted.Has(err) {
					worker.log.Error("failed hash verification.", zap.Stringer("satellite ID", worker.satelliteID), zap.Stringer("piece ID", pieceID), zap.Error(errs.Wrap(err)))
					worker.handleFailure(ctx, pb.TransferFailed_HASH_VERIFICATION, pieceID, c.Send)
				} else {
					worker.log.Error("failed to put piece.", zap.Stringer("satellite ID", worker.satelliteID), zap.Stringer("piece ID", pieceID), zap.Error(errs.Wrap(err)))
					// TODO look at error type to decide on the transfer error
					worker.handleFailure(ctx, pb.TransferFailed_STORAGE_NODE_UNAVAILABLE, pieceID, c.Send)
				}
				continue
			}

			success := &pb.StorageNodeMessage{
				Message: &pb.StorageNodeMessage_Succeeded{
					Succeeded: &pb.TransferSucceeded{
						OriginalPieceId:      msg.TransferPiece.OriginalPieceId,
						OriginalPieceHash:    originalHash,
						OriginalOrderLimit:   originalOrderLimit,
						ReplacementPieceHash: pieceHash,
					},
				},
			}
			err = c.Send(success)
			if err != nil {
				return errs.Wrap(err)
			}
		case *pb.SatelliteMessage_DeletePiece:
			pieceID := msg.DeletePiece.OriginalPieceId
			err := worker.store.Delete(ctx, worker.satelliteID, pieceID)
			if err != nil {
				worker.log.Error("failed to delete piece.", zap.Stringer("satellite ID", worker.satelliteID), zap.Stringer("piece ID", pieceID), zap.Error(errs.Wrap(err)))
			}
		case *pb.SatelliteMessage_ExitFailed:
			worker.log.Error("graceful exit failed.", zap.Stringer("satellite ID", worker.satelliteID), zap.Stringer("reason", msg.ExitFailed.Reason))

			err = worker.satelliteDB.CompleteGracefulExit(ctx, worker.satelliteID, time.Now(), satellites.ExitFailed, msg.ExitFailed.GetExitFailureSignature())
			if err != nil {
				return errs.Wrap(err)
			}
			break
		case *pb.SatelliteMessage_ExitCompleted:
			worker.log.Info("graceful exit completed.", zap.Stringer("satellite ID", worker.satelliteID))

			err = worker.satelliteDB.CompleteGracefulExit(ctx, worker.satelliteID, time.Now(), satellites.ExitSucceeded, msg.ExitCompleted.GetExitCompleteSignature())
			if err != nil {
				return errs.Wrap(err)
			}
			break
		default:
			// TODO handle err
			worker.log.Error("unknown graceful exit message.", zap.Stringer("satellite ID", worker.satelliteID))
		}

	}

	return errs.Wrap(err)
}

func (worker *Worker) handleFailure(ctx context.Context, transferError pb.TransferFailed_Error, pieceID pb.PieceID, send func(*pb.StorageNodeMessage) error) {
	failure := &pb.StorageNodeMessage{
		Message: &pb.StorageNodeMessage_Failed{
			Failed: &pb.TransferFailed{
				OriginalPieceId: pieceID,
				Error:           transferError,
			},
		},
	}

	sendErr := send(failure)
	if sendErr != nil {
		worker.log.Error("unable to send failure.", zap.Stringer("satellite ID", worker.satelliteID))
	}
}

// Close halts the worker.
func (worker *Worker) Close() error {
	// TODO not sure this is needed yet.
	return nil
}

// TODO This comes from piecestore.Endpoint. It should probably be an exported method so I don't have to duplicate it here.
func (worker *Worker) getHashAndLimit(ctx context.Context, pieceReader *pieces.Reader, limit *pb.OrderLimit) (pieceHash *pb.PieceHash, orderLimit *pb.OrderLimit, err error) {

	if pieceReader.StorageFormatVersion() == 0 {
		// v0 stores this information in SQL
		info, err := worker.store.GetV0PieceInfoDB().Get(ctx, limit.SatelliteId, limit.PieceId)
		if err != nil {
			worker.log.Error("error getting piece from v0 pieceinfo db", zap.Error(err))
			return nil, nil, err
		}
		orderLimit = info.OrderLimit
		pieceHash = info.UplinkPieceHash
	} else {
		//v1+ stores this information in the file
		header, err := pieceReader.GetPieceHeader()
		if err != nil {
			worker.log.Error("error getting header from piecereader", zap.Error(err))
			return nil, nil, err
		}
		orderLimit = &header.OrderLimit
		pieceHash = &pb.PieceHash{
			PieceId:   orderLimit.PieceId,
			Hash:      header.GetHash(),
			PieceSize: pieceReader.Size(),
			Timestamp: header.GetCreationTime(),
			Signature: header.GetSignature(),
		}
	}

	return
}
