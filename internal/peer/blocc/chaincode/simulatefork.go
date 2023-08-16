package chaincode

import (
	"context"
	"crypto/tls"
	cb "github.com/hyperledger/fabric-protos-go/common"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/bccsp"
	"github.com/hyperledger/fabric/internal/peer/chaincode"
	"github.com/hyperledger/fabric/internal/peer/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"time"
)

type SimulateForkAttempt struct {
	Certificate     tls.Certificate
	Command         *cobra.Command
	BroadcastClient common.BroadcastClient
	DeliverClients  []pb.DeliverClient
	EndorserClients []EndorserClient
	Input           *SimulateForkAttemptInput
	Signer          Signer
}

type SimulateForkAttemptInput struct {
	OrdererAddress        string
	RootCertFilePath      string
	ChannelID             string
	PeerAddress           string
	ConnectionProfilePath string
	WaitForEvent          bool
	WaitForEventTimeout   time.Duration
}

func (s *SimulateForkAttemptInput) Validate() error {
	if s.ChannelID == "" {
		return errors.New("ChannelID not specified")
	}
	if s.PeerAddress == "" {
		return errors.New("PeerAddresses not specified")
	}
	if s.OrdererAddress == "" {
		return errors.New("OrdererAddress not specified")
	}
	if s.RootCertFilePath == "" {
		return errors.New("RootCertFilePath not specified")
	}
	return nil
}

func SimulateForkAttemptCmd(s *SimulateForkAttempt, cryptoProvider bccsp.BCCSP) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "simulatefork",
		Short: "Simulate a fork attempt",
		Long:  "Simulate a fork attempt by including a special code in the ChaincodeInvocationSpec",
		RunE: func(cmd *cobra.Command, args []string) error {
			if s == nil {
				input := s.createInput()

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

				s = &SimulateForkAttempt{
					Command:         cmd,
					Input:           input,
					Certificate:     cc.Certificate,
					BroadcastClient: cc.BroadcastClient,
					DeliverClients:  cc.DeliverClients,
					EndorserClients: endorserClients,
					Signer:          cc.Signer,
				}
			}
			return s.SimulateForkAttempt()
		},
	}
	flagList := []string{
		"ordererAddress",
		"rootCertFilePath",
		"channelID",
		"peerAddress",
		"tlsRootCertFile",
		"connectionProfile",
		"waitForEvent",
		"waitForEventTimeout",
	}
	attachFlags(cmd, flagList)

	return cmd
}

func (s *SimulateForkAttempt) SimulateForkAttempt() error {
	err := s.Input.Validate()
	if err != nil {
		return err
	}

	if s.Command != nil {
		// Parsing of the command line is done so silence cmd usage
		s.Command.SilenceUsage = true
	}

	proposal, txIDSubmission, err := s.createProposal()
	if err != nil {
		return errors.WithMessage(err, "failed to create proposal")
	}

	signedProposal, err := signProposal(proposal, s.Signer)
	if err != nil {
		return errors.WithMessage(err, "failed to create signed proposal")
	}

	var responses []*pb.ProposalResponse
	for _, endorser := range s.EndorserClients {
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
	env, err := protoutil.CreateSignedTx(proposal, s.Signer, responses...)
	if err != nil {
		return errors.WithMessage(err, "failed to create signed transaction")
	}
	var dg *chaincode.DeliverGroup
	var ctx context.Context
	if s.Input.WaitForEvent {
		var cancelFunc context.CancelFunc
		ctx, cancelFunc = context.WithTimeout(context.Background(), s.Input.WaitForEventTimeout)
		defer cancelFunc()

		dg = chaincode.NewDeliverGroup(
			s.DeliverClients,
			[]string{s.Input.PeerAddress},
			s.Signer,
			s.Certificate,
			s.Input.ChannelID,
			txIDSubmission,
		)
		// connect to deliver service on all peers
		err := dg.Connect(ctx)
		if err != nil {
			return err
		}
	}

	if err = s.BroadcastClient.Send(env); err != nil {
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

func (s *SimulateForkAttempt) createInput() *SimulateForkAttemptInput {
	return &SimulateForkAttemptInput{
		OrdererAddress:      ordererAddress,
		RootCertFilePath:    rootCertFilePath,
		ChannelID:           channelID,
		WaitForEvent:        waitForEvent,
		WaitForEventTimeout: waitForEventTimeout,
		PeerAddress:         peerAddress,
	}
}

func (s *SimulateForkAttempt) createProposal() (proposal *pb.Proposal, txID string, err error) {
	if s.Signer == nil {
		return nil, "", errors.New("nil signer provided")
	}

	if err != nil {
		return nil, "", err
	}
	ccInput := &pb.ChaincodeInput{
		Args: append([][]byte{[]byte(simulateFuncName)}, nil),
	}

	cis := &pb.ChaincodeInvocationSpec{
		ChaincodeSpec: &pb.ChaincodeSpec{
			ChaincodeId: &pb.ChaincodeID{Name: bloccName},
			Input:       ccInput,
		},
	}

	creatorBytes, err := s.Signer.Serialize()
	if err != nil {
		return nil, "", errors.WithMessage(err, "failed to serialize identity")
	}

	proposal, txID, err = protoutil.CreateChaincodeProposalWithTxIDAndTransient(
		cb.HeaderType_ENDORSER_TRANSACTION,
		s.Input.ChannelID,
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
