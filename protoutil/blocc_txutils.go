package protoutil

import (
	"encoding/base64"
	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/msp"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/pkg/errors"
	"strconv"
)

func extractChaincodeInvocationSpec(envelopeBytes []byte) (*peer.ChaincodeInvocationSpec, error) {
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

// ExtractApprovalInfo returns the MSP ID of the peer and the TxID of
// a sensor reading transaction that it approves with BSCC transaction
func ExtractApprovalInfo(envelopeBytes []byte) (string, string, error) {
	if envelopeBytes == nil {
		return "", "", errors.New("envelopeBytes should not be nil")
	}

	isBscc, err := IsBscc(envelopeBytes)
	if err != nil {
		return "", "", err
	}

	if !isBscc {
		return "", "", errors.New("The given transaction is not a BSCC transaction")
	}

	cis, err := extractChaincodeInvocationSpec(envelopeBytes)
	if err != nil {
		return "", "", err
	}
	approvedTxID := cis.ChaincodeSpec.Input.Args[1]

	decodedApprovedTxID, err := base64.StdEncoding.DecodeString(string(approvedTxID))

	mspId, err := ExtractMspIdFromEnvelope(envelopeBytes)

	if err != nil {
		return "", "", err
	}

	return mspId, string(decodedApprovedTxID), nil
}

func IsBscc(envelopeBytes []byte) (bool, error) {
	if envelopeBytes == nil {
		return false, errors.New("envelopeBytes should not be nil")
	}

	cis, err := extractChaincodeInvocationSpec(envelopeBytes)
	if err != nil {
		return false, err
	}
	chaincodeName := cis.ChaincodeSpec.ChaincodeId.Name

	return chaincodeName == "bscc", nil
}

func ExtractMspIdFromEnvelope(envelopeBytes []byte) (string, error) {
	if envelopeBytes == nil {
		return "", errors.New("envelopeBytes should not be nil")
	}

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

// ExtractTemperatureHumidityReadingFromEnvelope retrieve the temperature, relative humidity, timestamp
// from a TemperatureHumidityReadingContract transaction
func ExtractTemperatureHumidityReadingFromEnvelope(envelope *common.Envelope) (float64, float64, int64, error) {
	if envelope == nil {
		return 0, 0, 0, errors.New("envelope should not be nil")
	}

	// Unmarshal the payload
	payload, err := UnmarshalPayload(envelope.GetPayload())
	if err != nil {
		return 0, 0, 0, err
	}

	// Unmarshal the transaction
	tx, err := UnmarshalTransaction(payload.Data)
	if err != nil {
		return 0, 0, 0, err
	}

	// Assuming the transaction has at least one action
	if len(tx.Actions) == 0 {
		return 0, 0, 0, errors.New("no transaction actions found")
	}

	// Unmarshal the ChaincodeActionPayload
	ccActionPayload, err := UnmarshalChaincodeActionPayload(tx.GetActions()[0].GetPayload())
	if err != nil {
		return 0, 0, 0, err
	}

	// Unmarshal the ChaincodeProposalPayload
	ccProposalPayload, err := UnmarshalChaincodeProposalPayload(ccActionPayload.GetChaincodeProposalPayload())
	if err != nil {
		return 0, 0, 0, err
	}

	// Unmarshal and return the ChaincodeInvocationSpec
	ccInvocationSpec, err := UnmarshalChaincodeInvocationSpec(ccProposalPayload.Input)
	if err != nil {
		return 0, 0, 0, err
	}

	temperatureBytes, err := base64.StdEncoding.DecodeString(string(ccInvocationSpec.ChaincodeSpec.Input.Args[1]))
	if err != nil {
		return 0, 0, 0, err
	}
	relativeHumidityBytes, err := base64.StdEncoding.DecodeString(string(ccInvocationSpec.ChaincodeSpec.Input.Args[2]))
	if err != nil {
		return 0, 0, 0, err
	}
	timestampBytes, err := base64.StdEncoding.DecodeString(string(ccInvocationSpec.ChaincodeSpec.Input.Args[3]))
	if err != nil {
		return 0, 0, 0, err
	}

	temperature, err := strconv.ParseFloat(string(temperatureBytes), 64)
	if err != nil {
		return 0, 0, 0, err
	}
	relativeHumidity, err := strconv.ParseFloat(string(relativeHumidityBytes), 64)
	if err != nil {
		return 0, 0, 0, err
	}
	timestamp, err := strconv.ParseInt(string(timestampBytes), 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}

	return temperature, relativeHumidity, timestamp, nil
}
