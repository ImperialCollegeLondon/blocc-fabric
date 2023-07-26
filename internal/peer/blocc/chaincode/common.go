/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package chaincode

import (
	"context"

	"github.com/golang/protobuf/proto"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

// EndorserClient defines the interface for sending a proposal
// to an endorser
type EndorserClient interface {
	ProcessProposal(ctx context.Context, in *pb.SignedProposal, opts ...grpc.CallOption) (*pb.ProposalResponse, error)
}

// PeerDeliverClient defines the interface for a peer deliver client
type PeerDeliverClient interface {
	Deliver(ctx context.Context, opts ...grpc.CallOption) (pb.Deliver_DeliverClient, error)
	DeliverFiltered(ctx context.Context, opts ...grpc.CallOption) (pb.Deliver_DeliverClient, error)
}

// Signer defines the interface needed for signing messages
type Signer interface {
	Sign(msg []byte) ([]byte, error)
	Serialize() ([]byte, error)
}

// Writer defines the interface needed for writing a file
type Writer interface {
	WriteFile(string, string, []byte) error
}

func signProposal(proposal *pb.Proposal, signer Signer) (*pb.SignedProposal, error) {
	// check for nil argument
	if proposal == nil {
		return nil, errors.New("proposal cannot be nil")
	}

	if signer == nil {
		return nil, errors.New("signer cannot be nil")
	}

	proposalBytes, err := proto.Marshal(proposal)
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling proposal")
	}

	signature, err := signer.Sign(proposalBytes)
	if err != nil {
		return nil, err
	}

	return &pb.SignedProposal{
		ProposalBytes: proposalBytes,
		Signature:     signature,
	}, nil
}
