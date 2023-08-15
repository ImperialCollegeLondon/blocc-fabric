/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package qscc

import (
	"encoding/json"
	"fmt"
	"github.com/hyperledger/fabric/protoutil"
	"strconv"

	"github.com/hyperledger/fabric-chaincode-go/shim"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/aclmgmt"
	"github.com/hyperledger/fabric/core/ledger"
)

// LedgerGetter gets the PeerLedger associated with a channel.
type LedgerGetter interface {
	GetLedger(cid string) ledger.PeerLedger
}

type TemperatureHumidityReading struct {
	Temperature      float64 `json:"temperature"`
	RelativeHumidity float64 `json:"relativeHumidity"`
	Timestamp        int64   `json:"timestamp"`
}

type OutputEntry struct {
	TxID            string                     `json:"txID"`
	ApprovingMspIDs []string                   `json:"approvingMspIDs"`
	Reading         TemperatureHumidityReading `json:"reading"`
}

// New returns an instance of QSCC.
// Typically this is called once per peer.
func New(aclProvider aclmgmt.ACLProvider, ledgers LedgerGetter) *LedgerQuerier {
	return &LedgerQuerier{
		aclProvider: aclProvider,
		ledgers:     ledgers,
	}
}

func (e *LedgerQuerier) Name() string              { return "qscc" }
func (e *LedgerQuerier) Chaincode() shim.Chaincode { return e }

// LedgerQuerier implements the ledger query functions, including:
// - GetChainInfo returns BlockchainInfo
// - GetBlockByNumber returns a block
// - GetBlockByHash returns a block
// - GetTransactionByID returns a transaction
type LedgerQuerier struct {
	aclProvider aclmgmt.ACLProvider
	ledgers     LedgerGetter
}

var qscclogger = flogging.MustGetLogger("qscc")

// These are function names from Invoke first parameter
const (
	GetChainInfo            string = "GetChainInfo"
	GetBlockByNumber        string = "GetBlockByNumber"
	GetBlockByHash          string = "GetBlockByHash"
	GetTransactionByID      string = "GetTransactionByID"
	GetBlockByTxID          string = "GetBlockByTxID"
	GetApprovedTransactions string = "GetApprovedTransactions"
)

// Init is called once per chain when the chain is created.
// This allows the chaincode to initialize any variables on the ledger prior
// to any transaction execution on the chain.
func (e *LedgerQuerier) Init(stub shim.ChaincodeStubInterface) pb.Response {
	qscclogger.Info("Init QSCC")

	return shim.Success(nil)
}

// Invoke is called with args[0] contains the query function name, args[1]
// contains the chain ID, which is temporary for now until it is part of stub.
// Each function requires additional parameters as described below:
// # GetChainInfo: Return a BlockchainInfo object marshalled in bytes
// # GetBlockByNumber: Return the block specified by block number in args[2]
// # GetBlockByHash: Return the block specified by block hash in args[2]
// # GetTransactionByID: Return the transaction specified by ID in args[2]
func (e *LedgerQuerier) Invoke(stub shim.ChaincodeStubInterface) pb.Response {
	args := stub.GetArgs()

	if len(args) < 2 {
		return shim.Error(fmt.Sprintf("Incorrect number of arguments, %d", len(args)))
	}

	fname := string(args[0])
	cid := string(args[1])

	sp, err := stub.GetSignedProposal()
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed getting signed proposal from stub, %s: %s", cid, err))
	}

	name, err := protoutil.InvokedChaincodeName(sp.ProposalBytes)
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed to identify the called chaincode: %s", err))
	}

	if name != e.Name() {
		return shim.Error(fmt.Sprintf("Rejecting invoke of QSCC from another chaincode because of potential for deadlocks, original invocation for '%s'", name))
	}

	if fname != GetChainInfo && len(args) < 3 {
		return shim.Error(fmt.Sprintf("missing 3rd argument for %s", fname))
	}

	targetLedger := e.ledgers.GetLedger(cid)
	if targetLedger == nil {
		return shim.Error(fmt.Sprintf("Invalid chain ID, %s", cid))
	}

	qscclogger.Debugf("Invoke function: %s on chain: %s", fname, cid)

	// Handle ACL:
	res := getACLResource(fname)
	if err = e.aclProvider.CheckACL(res, cid, sp); err != nil {
		return shim.Error(fmt.Sprintf("access denied for [%s][%s]: [%s]", fname, cid, err))
	}

	switch fname {
	case GetTransactionByID:
		return getTransactionByID(targetLedger, args[2])
	case GetBlockByNumber:
		return getBlockByNumber(targetLedger, args[2])
	case GetBlockByHash:
		return getBlockByHash(targetLedger, args[2])
	case GetChainInfo:
		return getChainInfo(targetLedger)
	case GetBlockByTxID:
		return getBlockByTxID(targetLedger, args[2])
	case GetApprovedTransactions:
		return getApprovedTransactions(targetLedger, args[2])
	}

	return shim.Error(fmt.Sprintf("Requested function %s not found.", fname))
}

// getApprovedTransactions queries the ledger for transactions that have been approved by BSCC (Blockchain System Chaincode).
// For each approved transaction, it retrieves the associated MSP IDs that have approved the transaction and the corresponding
// temperature and humidity readings. The function returns a JSON array where each entry contains the transaction ID (TxID),
// a list of approving MSP IDs, and the associated reading.
//
// Parameters:
// - vledger: The peer ledger to query.
// - chaincodeName: The name of the chaincode for which to retrieve approved transactions.
//
// Returns:
// - A successful response contains a JSON array of approved transactions with their associated data.
// - An error response contains a description of the error.
//
// Example of a returned JSON array:
// [
//
//	{
//	  "txID": "tx12345",
//	  "approvingMspIDs": ["Org1MSP", "Org2MSP"],
//	  "reading": {
//	    "temperature": 22.5,
//	    "relativeHumidity": 0.6,
//	    "timestamp": 1628887200
//	  }
//	},
//	...
//
// ]
func getApprovedTransactions(vledger ledger.PeerLedger, chaincodeName []byte) pb.Response {
	if chaincodeName == nil {
		return shim.Error("Chaincode name must not be nil.")
	}

	ccName := string(chaincodeName)
	qscclogger.Infof("BLOCC: Querying approved transactions for chaincode %s", ccName)

	agreements := make(map[string]OutputEntry)

	binfo, err := vledger.GetBlockchainInfo()
	if err != nil {
		errMsg := fmt.Sprintf("BLOCC: Failed to get chain info, error %s", err)
		qscclogger.Error(errMsg)
		return shim.Error(errMsg)
	}

	height := binfo.Height

	for blockNum := uint64(0); blockNum < height; blockNum++ {
		qscclogger.Debugf("BLOCC: checking block %d", blockNum)

		block, err := vledger.GetBlockByNumber(blockNum)
		if err != nil {
			errMsg := fmt.Sprintf("BLOCC: Failed to get block number %d, error %s", blockNum, err)
			qscclogger.Error(errMsg)
			return shim.Error(errMsg)
		}

		// End of ledger reached
		if block == nil {
			break
		}

		for _, data := range block.Data.Data {
			isBscc, err := protoutil.IsBscc(data)
			if err != nil {
				errMsg := fmt.Sprintf("BLOCC: Failed to identify BSCC transaction, error %s", err)
				qscclogger.Error(errMsg)
				return shim.Error(errMsg)
			}

			// Skip non-BSCC transactions
			if !isBscc {
				continue
			}

			qscclogger.Debugf("BLOCC: Block %d is a BSCC block", blockNum)

			mspId, approvedTxId, err := protoutil.ExtractApprovalInfo(data)
			if err != nil {
				errMsg := fmt.Sprintf("BLOCC: Failed to extract approval info, error %s", err)
				qscclogger.Error(errMsg)
				return shim.Error(errMsg)
			}

			entry, exists := agreements[approvedTxId]
			if exists {
				entry.ApprovingMspIDs = append(entry.ApprovingMspIDs, mspId)
				agreements[approvedTxId] = entry
				continue
			}

			// Record reading if the approved transaction is unseen
			sensorTransaction, err := vledger.GetTransactionByID(approvedTxId)
			if err != nil {
				errMsg := fmt.Sprintf("BLOCC: Failed to get transaction by ID %s, error %s", approvedTxId, err)
				qscclogger.Error(errMsg)
				return shim.Error(errMsg)
			}

			temperature, relativeHumidity, timestamp, err := protoutil.ExtractTemperatureHumidityReadingFromEnvelope(sensorTransaction.GetTransactionEnvelope())
			if err != nil {
				errMsg := fmt.Sprintf("BLOCC: Failed to fetch reading from approved transaction %s, error %s", approvedTxId, err)
				qscclogger.Error(errMsg)
				return shim.Error(errMsg)
			}

			qscclogger.Debugf("BLOCC: block %d, approvingMspId=%s, approvedTxId=%s, temperature=%d, relativeHumidity=%d, timestamp=%d",
				blockNum, mspId, approvedTxId, temperature, relativeHumidity, timestamp)

			agreements[approvedTxId] = OutputEntry{
				TxID:            approvedTxId,
				ApprovingMspIDs: []string{mspId},
				Reading: TemperatureHumidityReading{
					Temperature:      temperature,
					RelativeHumidity: relativeHumidity,
					Timestamp:        timestamp,
				},
			}
		}

	}

	// Initialise to an empty array
	result := make([]OutputEntry, 0)
	for _, entry := range agreements {
		result = append(result, entry)
	}

	jsonResponse, err := json.Marshal(result)
	if err != nil {
		errMsg := fmt.Sprintf("BLOCC: Failed to marshal the result to JSON, error %s", err)
		qscclogger.Error(errMsg)
		return shim.Error(errMsg)
	}

	return shim.Success(jsonResponse)

}

func getTransactionByID(vledger ledger.PeerLedger, tid []byte) pb.Response {
	if tid == nil {
		return shim.Error("Transaction ID must not be nil.")
	}

	processedTran, err := vledger.GetTransactionByID(string(tid))
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed to get transaction with id %s, error %s", string(tid), err))
	}

	bytes, err := protoutil.Marshal(processedTran)
	if err != nil {
		return shim.Error(err.Error())
	}

	return shim.Success(bytes)
}

func getBlockByNumber(vledger ledger.PeerLedger, number []byte) pb.Response {
	if number == nil {
		return shim.Error("Block number must not be nil.")
	}
	bnum, err := strconv.ParseUint(string(number), 10, 64)
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed to parse block number with error %s", err))
	}
	block, err := vledger.GetBlockByNumber(bnum)
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed to get block number %d, error %s", bnum, err))
	}
	// TODO: consider trim block content before returning
	//  Specifically, trim transaction 'data' out of the transaction array Payloads
	//  This will preserve the transaction Payload header,
	//  and client can do GetTransactionByID() if they want the full transaction details

	bytes, err := protoutil.Marshal(block)
	if err != nil {
		return shim.Error(err.Error())
	}

	return shim.Success(bytes)
}

func getBlockByHash(vledger ledger.PeerLedger, hash []byte) pb.Response {
	if hash == nil {
		return shim.Error("Block hash must not be nil.")
	}
	block, err := vledger.GetBlockByHash(hash)
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed to get block hash %s, error %s", string(hash), err))
	}
	// TODO: consider trim block content before returning
	//  Specifically, trim transaction 'data' out of the transaction array Payloads
	//  This will preserve the transaction Payload header,
	//  and client can do GetTransactionByID() if they want the full transaction details

	bytes, err := protoutil.Marshal(block)
	if err != nil {
		return shim.Error(err.Error())
	}

	return shim.Success(bytes)
}

func getChainInfo(vledger ledger.PeerLedger) pb.Response {
	binfo, err := vledger.GetBlockchainInfo()
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed to get block info with error %s", err))
	}
	bytes, err := protoutil.Marshal(binfo)
	if err != nil {
		return shim.Error(err.Error())
	}

	return shim.Success(bytes)
}

func getBlockByTxID(vledger ledger.PeerLedger, rawTxID []byte) pb.Response {
	txID := string(rawTxID)
	block, err := vledger.GetBlockByTxID(txID)
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed to get block for txID %s, error %s", txID, err))
	}

	bytes, err := protoutil.Marshal(block)
	if err != nil {
		return shim.Error(err.Error())
	}

	return shim.Success(bytes)
}

func getACLResource(fname string) string {
	return "qscc/" + fname
}
