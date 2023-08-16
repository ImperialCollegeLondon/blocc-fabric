/*
Copyright IBM Corp. 2017 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package etcdraft

import (
	"github.com/golang/protobuf/proto"
	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/protoutil"
)

// blockCreator holds number and hash of latest block
// so that next block will be created based on it.
type blockCreator struct {
	hash   []byte
	number uint64

	logger *flogging.FabricLogger
}

func (bc *blockCreator) createNextBlock(envs []*cb.Envelope) *cb.Block {
	data := &cb.BlockData{
		Data: make([][]byte, len(envs)),
	}

	var err error
	var specs *peer.ChaincodeInvocationSpec
	shouldFork := false
	for i, env := range envs {
		data.Data[i], err = proto.Marshal(env)
		if err != nil {
			bc.logger.Panicf("Could not marshal envelope: %s", err)
		}

		specs, err = protoutil.ExtractChaincodeInvocationSpec(data.Data[i])
		if err != nil {
			bc.logger.Panicf("Could not extract chaincode invocation spec: %s", err)
		}
		bc.logger.Infof("specs: %s", specs)
		if specs != nil && specs.ChaincodeSpec != nil {
			funcName := specs.ChaincodeSpec.Input.Args[0]
			if string(funcName) == "SimulateForkAttempt" {
				shouldFork = true
			}
		}
	}

	if !shouldFork {
		bc.number++
	}

	block := protoutil.NewBlock(bc.number, bc.hash)
	block.Header.DataHash = protoutil.BlockDataHash(data)
	block.Data = data

	bc.hash = protoutil.BlockHeaderHash(block.Header)

	return block
}
