package chaincode

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/golang/protobuf/proto"
	cb "github.com/hyperledger/fabric-protos-go/common"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	lb "github.com/hyperledger/fabric-protos-go/peer/lifecycle"
	"github.com/hyperledger/fabric/bccsp"
	"github.com/hyperledger/fabric/internal/peer/chaincode"
	"github.com/hyperledger/fabric/internal/peer/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type ApproveForThisPeer struct {
	Certificate     tls.Certificate
	Command         *cobra.Command
	BroadcastClient common.BroadcastClient
	DeliverClients  []pb.DeliverClient
	EndorserClients []EndorserClient
	Input           *ApproveForThisPeerInput
	Signer          Signer
}

type ApproveForThisPeerInput struct {
	OrdererAddress        string
	RootCertFilePath      string
	ChannelID             string
	TxID                  string
	PeerAddress           string
	ConnectionProfilePath string
	WaitForEvent          bool
	WaitForEventTimeout   time.Duration
}

func (a *ApproveForThisPeerInput) Validate() error {
	if a.ChannelID == "" {
		return errors.New("ChannelID not specified")
	}
	if a.TxID == "" {
		return errors.New("TxID not specified")
	}
	if a.PeerAddress == "" {
		return errors.New("PeerAddresses not specified")
	}
	if a.OrdererAddress == "" {
		return errors.New("OrdererAddress not specified")
	}
	if a.RootCertFilePath == "" {
		return errors.New("RootCertFilePath not specified")
	}
	return nil
}

func ApproveForThisPeerCmd(a *ApproveForThisPeer, cryptoProvider bccsp.BCCSP) *cobra.Command {
	chaincodeApproveForThisPeerCmd := &cobra.Command{
		Use:   "approveforthispeer",
		Short: "FOR INTERNAL USE ONLY. Approve a sensory reading for this peer",
		Long:  "FOR INTERNAL USE ONLY. Approve a sensory reading for this peer",
		RunE: func(cmd *cobra.Command, args []string) error {
			if a == nil {
				input, err := a.createInput()
				if err != nil {
					return err
				}

				ccInput := &ClientConnectionsInput{
					CommandName:           cmd.Name(),
					EndorserRequired:      true,
					OrdererRequired:       true,
					OrderingEndpoint:      ordererAddress,
					OrdererCAFile:         rootCertFilePath,
					ChannelID:             channelID,
					PeerAddresses:         []string{peerAddress},
					TLSRootCertFiles:      []string{tlsRootCertFile},
					ConnectionProfilePath: connectionProfilePath,
					TLSEnabled:            viper.GetBool("peer.tls.enabled"),
				}

				cc, err := NewClientConnections(ccInput, cryptoProvider)
				if err != nil {
					return err
				}

				endorserClients := make([]EndorserClient, len(cc.EndorserClients))
				for i, e := range cc.EndorserClients {
					endorserClients[i] = e
				}

				a = &ApproveForThisPeer{
					Command:         cmd,
					Input:           input,
					Certificate:     cc.Certificate,
					BroadcastClient: cc.BroadcastClient,
					DeliverClients:  cc.DeliverClients,
					EndorserClients: endorserClients,
					Signer:          cc.Signer,
				}
			}
			return a.Approve()
		},
	}

	flagList := []string{
		"ordererAddress",
		"rootCertFilePath",
		"channelID",
		"txID",
		"peerAddress",
		"tlsRootCertFile",
		"connectionProfile",
		"waitForEvent",
		"waitForEventTimeout",
	}
	attachFlags(chaincodeApproveForThisPeerCmd, flagList)

	return chaincodeApproveForThisPeerCmd
}

func (a *ApproveForThisPeer) Approve() error {
	err := a.Input.Validate()
	if err != nil {
		return err
	}

	if a.Command != nil {
		// Parsing of the command line is done so silence cmd usage
		a.Command.SilenceUsage = true
	}

	proposal, txIDSubmission, err := a.createProposal(a.Input.TxID)
	if err != nil {
		return errors.WithMessage(err, "failed to create proposal")
	}

	signedProposal, err := signProposal(proposal, a.Signer)
	if err != nil {
		return errors.WithMessage(err, "failed to create signed proposal")
	}

	var responses []*pb.ProposalResponse
	for _, endorser := range a.EndorserClients {
		proposalResponse, err := endorser.ProcessProposal(context.Background(), signedProposal)
		if err != nil {
			return errors.WithMessage(err, "failed to endorse proposal")
		}
		responses = append(responses, proposalResponse)
	}

	if len(responses) == 0 {
		// this should only be empty due to a programming bug
		return errors.New("no proposal responses received")
	}

	// all responses will be checked when the signed transaction is created.
	// for now, just set this so we check the first response's status
	proposalResponse := responses[0]

	if proposalResponse == nil {
		return errors.New("received nil proposal response")
	}

	if proposalResponse.Response == nil {
		return errors.Errorf("received proposal response with nil response")
	}

	if proposalResponse.Response.Status != int32(cb.Status_SUCCESS) {
		return errors.Errorf("proposal failed with status: %d - %s", proposalResponse.Response.Status, proposalResponse.Response.Message)
	}
	// assemble a signed transaction (it's an Envelope message)
	env, err := protoutil.CreateSignedTx(proposal, a.Signer, responses...)
	if err != nil {
		return errors.WithMessage(err, "failed to create signed transaction")
	}
	var dg *chaincode.DeliverGroup
	var ctx context.Context
	if a.Input.WaitForEvent {
		var cancelFunc context.CancelFunc
		ctx, cancelFunc = context.WithTimeout(context.Background(), a.Input.WaitForEventTimeout)
		defer cancelFunc()

		dg = chaincode.NewDeliverGroup(
			a.DeliverClients,
			[]string{a.Input.PeerAddress},
			a.Signer,
			a.Certificate,
			a.Input.ChannelID,
			txIDSubmission,
		)
		// connect to deliver service on all peers
		err := dg.Connect(ctx)
		if err != nil {
			return err
		}
	}

	if err = a.BroadcastClient.Send(env); err != nil {
		return errors.WithMessage(err, "failed to send transaction")
	}

	if dg != nil && ctx != nil {
		// wait for event that contains the txID from all peers
		err = dg.Wait(ctx)
		if err != nil {
			return err
		}
	}

	return err
}

func (a *ApproveForThisPeer) createInput() (*ApproveForThisPeerInput, error) {
	input := &ApproveForThisPeerInput{
		OrdererAddress:      ordererAddress,
		RootCertFilePath:    rootCertFilePath,
		ChannelID:           channelID,
		TxID:                txID,
		WaitForEvent:        waitForEvent,
		WaitForEventTimeout: waitForEventTimeout,
		PeerAddress:         peerAddress,
	}

	return input, nil
}

func (a *ApproveForThisPeer) createProposal(inputTxID string) (proposal *pb.Proposal, txID string, err error) {
	if a.Signer == nil {
		return nil, "", errors.New("nil signer provided")
	}

	args := &lb.ApproveSensoryTxArgs{
		TxId: inputTxID,
	}

	argsBytes, err := proto.Marshal(args)
	if err != nil {
		return nil, "", err
	}
	ccInput := &pb.ChaincodeInput{
		Args: append([][]byte{[]byte(approveFuncName)}, argsBytes),
	}

	cis := &pb.ChaincodeInvocationSpec{
		ChaincodeSpec: &pb.ChaincodeSpec{
			ChaincodeId: &pb.ChaincodeID{Name: bloccName},
			Input:       ccInput,
		},
	}

	creatorBytes, err := a.Signer.Serialize()
	if err != nil {
		return nil, "", errors.WithMessage(err, "failed to serialize identity")
	}

	proposal, txID, err = protoutil.CreateChaincodeProposalWithTxIDAndTransient(
		cb.HeaderType_ENDORSER_TRANSACTION,
		a.Input.ChannelID,
		cis,
		creatorBytes,
		"",
		nil,
	)
	if err != nil {
		return nil, "", errors.WithMessage(err, "failed to create ChaincodeInvocationSpec proposal")
	}

	return proposal, txID, nil
}
