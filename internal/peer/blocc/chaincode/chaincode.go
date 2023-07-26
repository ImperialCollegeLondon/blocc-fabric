/*
SPDX-License-Identifier: Apache-2.0
*/

package chaincode

import (
	"time"

	"github.com/hyperledger/fabric/bccsp"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/internal/peer/common"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	bloccName       = "bscc"
	approveFuncName = "ApproveSensoryReading"
)

var logger = flogging.MustGetLogger("cli.blocc.chaincode")

// Cmd returns the cobra command for Chaincode
func Cmd(cryptoProvider bccsp.BCCSP) *cobra.Command {
	chaincodeCmd.AddCommand(ApproveForThisPeerCmd(nil, cryptoProvider))

	logger.Debugf("bloccCmd: %v", chaincodeCmd)

	return chaincodeCmd
}

var (
	ordererAddress        string
	rootCertFilePath      string
	channelID             string
	txID                  string
	peerAddress           string
	tlsRootCertFile       string
	connectionProfilePath string
	waitForEvent          bool
	waitForEventTimeout   time.Duration
)

var chaincodeCmd = &cobra.Command{
	Use:   "bscc",
	Short: "Perform blocc protocol related operations, not intended to be called directly by end users",
	Long:  "Perform blocc protocol related operations, not intended to be called directly by end users",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		common.InitCmd(cmd, args)
	},
}

var flags *pflag.FlagSet

func init() {
	ResetFlags()
}

// ResetFlags resets the values of these flags to facilitate tests
func ResetFlags() {
	flags = &pflag.FlagSet{}

	flags.StringVarP(&ordererAddress, "ordererAddress", "o", "", "The address of the orderer to connect to")
	flags.StringVarP(&rootCertFilePath, "rootCertFilePath", "", "", "If TLS is enabled, the path to the TLS root cert file of the orderer to connect to")
	flags.StringVarP(&channelID, "channelID", "c", "", "The channel on which this command should be executed")
	flags.StringVarP(&txID, "txID", "t", "", "The transaction ID to approve using for this command")
	flags.StringVarP(&peerAddress, "peerAddress", "", "", "The address of the peer to connect to")
	flags.StringVarP(&tlsRootCertFile, "tlsRootCertFile", "", "",
		"If TLS is enabled, the paths to the TLS root cert files of the peers to connect to. The order and number of certs specified should match the --peerAddress flag")
	flags.StringVarP(&connectionProfilePath, "connectionProfile", "", "",
		"The fully qualified path to the connection profile that provides the necessary connection information for the network. Note: currently only supported for providing peer connection information")
	flags.BoolVar(&waitForEvent, "waitForEvent", true,
		"Whether to wait for the event from each peer's deliver filtered service signifying that the transaction has been committed successfully")
	flags.DurationVar(&waitForEventTimeout, "waitForEventTimeout", 30*time.Second,
		"Time to wait for the event from each peer's deliver filtered service signifying that the 'invoke' transaction has been committed successfully")
}

func attachFlags(cmd *cobra.Command, names []string) {
	cmdFlags := cmd.Flags()
	for _, name := range names {
		if flag := flags.Lookup(name); flag != nil {
			cmdFlags.AddFlag(flag)
		} else {
			logger.Fatalf("Could not find flag '%s' to attach to command '%s'", name, cmd.Name())
		}
	}
}
