/*
BLOCC Project
SPDX-License-Identifier: Apache-2.0
*/

package bscc

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/hyperledger/fabric-chaincode-go/shim"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/bccsp"
	event "github.com/hyperledger/fabric/common/blocc-events"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/peer"
	blocc "github.com/hyperledger/fabric/internal/peer/blocc/chaincode"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
)

func New(peerInstance *peer.Peer) *BSCC {
	return &BSCC{
		peerInstance: peerInstance,
	}
}

func (bscc *BSCC) Name() string {
	return "bscc"
}

func (bscc *BSCC) Chaincode() shim.Chaincode {
	return bscc
}

type BSCC struct {
	peerInstance *peer.Peer
	config       Config
}

type Config struct {
	PeerAddress    string
	TLSCertFile    string
	CryptoProvider bccsp.BCCSP
}

var bloccProtoLogger = flogging.MustGetLogger("bscc")

const (
	approveSensoryReading string = "ApproveSensoryReading"
	simulateForkAttempt   string = "SimulateForkAttempt"
	checkForkStatus       string = "CheckForkStatus"
)

// ------------------- Error handling ------------------- //

type InvalidFunctionError string

func (f InvalidFunctionError) Error() string {
	return fmt.Sprintf("invalid function to bscc: %s", string(f))
}

// -------------------- Stub Interface ------------------- //

func (bscc *BSCC) Init(stub shim.ChaincodeStubInterface) pb.Response {
	bloccProtoLogger.Info("Init BSCC")
	go func() {
		for _event := range event.GlobalEventBus.Subscribe() {
			bscc.processEvent(_event)
		}
	}()

	peerAddress, ok := os.LookupEnv("CORE_PEER_ADDRESS")
	if !ok {
		bloccProtoLogger.Error("CORE_PEER_ADDRESS is not set")
		return shim.Error("CORE_PEER_ADDRESS is not set")
	}

	tlsCertFile, ok := os.LookupEnv("CORE_PEER_TLS_ROOTCERT_FILE")
	if !ok {
		bloccProtoLogger.Error("CORE_PEER_TLS_ROOTCERT_FILE is not set")
		return shim.Error("CORE_PEER_TLS_ROOTCERT_FILE is not set")
	}

	bscc.config = Config{
		PeerAddress:    peerAddress,
		TLSCertFile:    tlsCertFile,
		CryptoProvider: bscc.peerInstance.CryptoProvider,
	}
	return shim.Success(nil)
}

// Invoke [BLOCC System CC] This function is not allowed for external calls, only internal calls are allowed.
func (bscc *BSCC) Invoke(stub shim.ChaincodeStubInterface) pb.Response {
	args := stub.GetArgs()
	var err error

	if len(args) < 2 {
		return shim.Error(fmt.Sprintf("Incorrect number of arguments, %d", len(args)))
	}

	fname := string(args[0])
	bloccProtoLogger.Infof("Invoke function: %s", fname)

	// Handle ACL:
	sp, err := stub.GetSignedProposal()
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed getting signed proposal from stub: [%s]", err))
	}

	name, err := protoutil.InvokedChaincodeName(sp.ProposalBytes)
	if err != nil {
		return shim.Error(fmt.Sprintf("Failed to identify the called chaincode: %s", err))
	}

	if name != bscc.Name() {
		return shim.Error(fmt.Sprintf("Rejecting invoke of CSCC from another chaincode, original invocation for '%s'", name))
	}

	switch fname {
	case approveSensoryReading:
		txID := args[1]
		bloccProtoLogger.Infof("ApproveSensoryReading for: %s", txID)
		return shim.Success(txID)
	case simulateForkAttempt:
		bloccProtoLogger.Warningf("Adding a fork block!")
		return shim.Success(nil)
	case checkForkStatus:
		bloccProtoLogger.Infof("Checking fork status")
		return bscc.CheckForkStatus(string(args[1]))
	}

	return shim.Error(fmt.Sprintf("Requested function %s not found.", fname))
}

// ----------------- BSCC Implementation ----------------- //

func (bscc *BSCC) processEvent(event event.Event) {
	var err error
	bloccProtoLogger.Info("BLOCC - Received approval event:", event)
	address, rootCertFile, err := bscc.gatherOrdererInfo(event.ChannelID)
	if err != nil {
		bloccProtoLogger.Errorf("Failed to gather orderer info: %s", err)
		return
	}

	rootCertFilePath, err := bscc.createTempFile(rootCertFile)
	if err != nil {
		bloccProtoLogger.Errorf("Failed to create temp file: %s", err)
		return
	}
	defer bscc.removeTempFile(rootCertFilePath)

	err = bscc.approveSensoryReading(address, rootCertFilePath, event)
	if err != nil {
		bloccProtoLogger.Errorf("Failed to approve sensory reading: %s", err)
	}
}

func (bscc *BSCC) gatherOrdererInfo(channelID string) (address string, rootCertFile []byte, err error) {
	_, ordererOrg, err := bscc.peerInstance.GetOrdererInfo(channelID)
	if err != nil {
		return "", nil, err
	}

	if len(ordererOrg) == 0 {
		return "", nil, errors.New("No orderer organization found")
	} else {
		for _, orderer := range ordererOrg {
			// TODO: This is a hack, we should not assume that the orderer has only one address and one root cert.
			// To be checked against multiple orderers.
			return orderer.Addresses[0], orderer.RootCerts[0], nil
		}
	}

	return "", nil, errors.New("Error occurred gathering orderer info")
}

func (bscc *BSCC) createTempFile(rootCertFile []byte) (string, error) {
	tempFile, err := ioutil.TempFile("", "rootCertFile")
	if err != nil {
		return "", err
	}

	_, err = tempFile.Write(rootCertFile)
	if err != nil {
		return "", err
	}

	err = tempFile.Close()
	if err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

func (bscc *BSCC) removeTempFile(filePath string) {
	if err := os.Remove(filePath); err != nil {
		bloccProtoLogger.Errorf("Failed to remove temp file: %s", err)
	}
}

func (bscc *BSCC) approveSensoryReading(address, rootCertFilePath string, event event.Event) error {
	approveForThisPeerCmd := blocc.ApproveForThisPeerCmd(nil, bscc.config.CryptoProvider)
	approveForThisPeerCmd.SetArgs([]string{
		"--ordererAddress=" + address,
		"--rootCertFilePath=" + rootCertFilePath,
		"--channelID=" + event.ChannelID,
		"--txID=" + event.SensoryTxID,
		"--peerAddress=" + bscc.config.PeerAddress,
		"--tlsRootCertFile=" + bscc.config.TLSCertFile,
	})
	err := approveForThisPeerCmd.Execute()
	approveForThisPeerCmd.ResetFlags()

	return err
}

func (bscc *BSCC) CheckForkStatus(channelID string) pb.Response {
	if channelID == "" {
		return shim.Error("ChannelID not specified")
	}

	var err error
	var isForked bool

	// Define the filename based on the channel ID
	filename := fmt.Sprintf("/var/hyperledger/production/ledgersData/chains/chains/%s/fork_info.txt", channelID)

	// Check if the file exists
	if _, err = os.Stat(filename); os.IsNotExist(err) {
		isForked = false
	} else {
		isForked = true
	}

	jsonResponse, err := json.Marshal(isForked)
	if err != nil {
		errMsg := fmt.Sprintf("BLOCC: Failed to marshal the result to JSON, error %s", err)
		bloccProtoLogger.Error(errMsg)
		return shim.Error(errMsg)
	}

	return shim.Success(jsonResponse)
}
