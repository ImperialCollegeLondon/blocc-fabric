package protoutil

import (
	"encoding/base64"
	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/msp"
	"github.com/pkg/errors"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
)

func ExtractChaincodeInvocationSpec(envelopeBytes []byte) (*peer.ChaincodeInvocationSpec, error) {
	// Unmarshal the envelope
	env := &common.Envelope{}
	if err := proto.Unmarshal(envelopeBytes, env); err != nil {
		return nil, err
	}

	// Unmarshal the payload
	payload := &common.Payload{}
	if err := proto.Unmarshal(env.Payload, payload); err != nil {
		return nil, err
	}

	// Unmarshal the transaction
	tx := &peer.Transaction{}
	if err := proto.Unmarshal(payload.Data, tx); err != nil {
		return nil, err
	}

	// Assuming the transaction has at least one action
	if len(tx.Actions) == 0 {
		return nil, errors.New("no transaction actions found")
	}

	// Unmarshal the ChaincodeActionPayload
	CAP := &peer.ChaincodeActionPayload{}
	if err := proto.Unmarshal(tx.Actions[0].Payload, CAP); err != nil {
		return nil, err
	}

	// Unmarshal the ChaincodeProposalPayload
	cpp := &peer.ChaincodeProposalPayload{}
	if err := proto.Unmarshal(CAP.ChaincodeProposalPayload, cpp); err != nil {
		return nil, err
	}

	// Unmarshal and return the ChaincodeInvocationSpec
	cis := &peer.ChaincodeInvocationSpec{}
	if err := proto.Unmarshal(cpp.Input, cis); err != nil {
		return nil, err
	}

	return cis, nil
}

func ExtractApprovalTxID(envelopeBytes []byte) (string, error) {
	cis, err := ExtractChaincodeInvocationSpec(envelopeBytes)
	if err != nil {
		return "", err
	}
	approvalTxID := cis.ChaincodeSpec.Input.Args[1]

	decodedApprovalTxID, err := base64.StdEncoding.DecodeString(string(approvalTxID))

	return string(decodedApprovalTxID), nil
}

func IsBscc(envelopeBytes []byte) (bool, error) {
	cis, err := ExtractChaincodeInvocationSpec(envelopeBytes)
	if err != nil {
		return false, err
	}
	chaincodeName := cis.ChaincodeSpec.ChaincodeId.Name

	return chaincodeName == "bscc", nil
}

func ExtractMspIdFromEnvelope(envelopeBytes []byte) (string, error) {
	// Unmarshal the envelope
	env := &common.Envelope{}
	if err := proto.Unmarshal(envelopeBytes, env); err != nil {
		return "", err
	}

	// Unmarshal the payload
	payload := &common.Payload{}
	if err := proto.Unmarshal(env.Payload, payload); err != nil {
		return "", err
	}

	// Extract the signature header
	sigHeader := &common.SignatureHeader{}
	if err := proto.Unmarshal(payload.Header.SignatureHeader, sigHeader); err != nil {
		return "", err
	}

	// Unmarshal the SerializedIdentity from the creator
	serializedIdentity := &msp.SerializedIdentity{}
	if err := proto.Unmarshal(sigHeader.Creator, serializedIdentity); err != nil {
		return "", err
	}

	// Return the MspId
	return serializedIdentity.Mspid, nil
}