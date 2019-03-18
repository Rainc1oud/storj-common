// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package signing

import (
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/storj"
)

// Signee is able to verify that the data signature belongs to the signee.
type Signee interface {
	ID() storj.NodeID
	HashAndVerifySignature(data, signature []byte) error
}

// VerifyOrderLimitSignature verifies that the signature inside order limit belongs to the satellite.
func VerifyOrderLimitSignature(satellite Signee, signed *pb.OrderLimit2) error {
	bytes, err := EncodeOrderLimit(signed)
	if err != nil {
		return Error.Wrap(err)
	}

	return satellite.HashAndVerifySignature(bytes, signed.SatelliteSignature)
}

// VerifyOrderSignature verifies that the signature inside order belongs to the uplink.
func VerifyOrderSignature(uplink Signee, signed *pb.Order2) error {
	bytes, err := EncodeOrder(signed)
	if err != nil {
		return Error.Wrap(err)
	}

	return uplink.HashAndVerifySignature(bytes, signed.UplinkSignature)
}

// VerifyPieceHashSignature verifies that the signature inside piece hash belongs to the signer, which is either uplink or storage node.
func VerifyPieceHashSignature(signee Signee, signed *pb.PieceHash) error {
	bytes, err := EncodePieceHash(signed)
	if err != nil {
		return Error.Wrap(err)
	}

	return signee.HashAndVerifySignature(bytes, signed.Signature)
}
