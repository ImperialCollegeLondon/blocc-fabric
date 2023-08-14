/*
Copyright IBM Corp. 2017 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package etcdraft

import (
	"encoding/base64"
	"github.com/golang/protobuf/proto"
	cb "github.com/hyperledger/fabric-protos-go/common"
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
	bc.logger.Debugf("Creating next block for chain with height %d", bc.number)
	data := &cb.BlockData{
		Data: make([][]byte, len(envs)),
	}

	var err error
	for i, env := range envs {
		data.Data[i], err = proto.Marshal(env)
		if err != nil {
			bc.logger.Panicf("Could not marshal envelope: %s", err)
		}
	}

	//bc.number++

	if bc.number == 15 {
		bc.number = 15
	} else {
		bc.number++
	}

	block := protoutil.NewBlock(bc.number, bc.hash)
	block.Header.DataHash = protoutil.BlockDataHash(data)
	block.Data = data

	bc.hash = protoutil.BlockHeaderHash(block.Header)

	bc.logger.Debug("Block header data hash: ", base64.StdEncoding.EncodeToString(block.Header.DataHash))
	bc.logger.Debug("Block header previous hash: ", base64.StdEncoding.EncodeToString(block.Header.PreviousHash))
	bc.logger.Debug("Block header hash: ", base64.StdEncoding.EncodeToString(bc.hash))
	return block
}
