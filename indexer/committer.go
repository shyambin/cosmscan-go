package indexer

import (
	"context"
	"cosmscan-go/db"
	"cosmscan-go/indexer/schema"
	"fmt"

	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	coretypes "github.com/tendermint/tendermint/rpc/core/types"
	"go.uber.org/zap"
)

type Committer struct {
	log        *zap.SugaredLogger
	ctx        context.Context
	cancelFunc context.CancelFunc
	storage    db.DB
}

func NewCommitter(storage db.DB) *Committer {
	ctx, cancel := context.WithCancel(context.Background())

	return &Committer{
		log:        zap.S().Named("committer"),
		ctx:        ctx,
		cancelFunc: cancel,
		storage:    storage,
	}
}

func (c *Committer) Run(blockCh chan *msgCommitBlock) {
	for {
		select {
		case <-c.ctx.Done():
			c.log.Info("committer is shutting down")
			return
		case block := <-blockCh:
			if err := c.commitBlock(block); err != nil {
				// sometimes database is temporarily unavailable, in the future, we need to retry
				c.log.Fatalw("failed to commit block, this is unexpected behavior", "err", err)
			}
		}
	}
}

func (c *Committer) Close() {
	c.cancelFunc()
}

func (c *Committer) commitBlock(block *msgCommitBlock) error {
	var abciTx []*coretypes.ResultTx
	var cosmTx []*txtypes.GetTxResponse

	for _, tx := range block.txs {
		abciTx = append(abciTx, tx.abci)
		cosmTx = append(cosmTx, tx.cosmos)
	}

	fullBlock, err := schema.NewFullBlock(block.block, abciTx, cosmTx)
	if err != nil {
		return fmt.Errorf("err creating full block: %w", err)
	}

	fullBlock.Block.ChainId = 1
	if _, err := c.storage.InsertBlock(c.ctx, fullBlock.Block); err != nil {
		return fmt.Errorf("err insert block: %w", err)
	}

	for _, transaction := range fullBlock.Txs {
		transaction.Tx.ChainId = 1
		txId, err := c.storage.InsertTransaction(c.ctx, transaction.Tx)
		if err != nil {
			return fmt.Errorf("err insert transaction: %w", err)
		}

		for _, evt := range transaction.Events {
			evt.ChainId = 1
			evt.TxId = int(txId)
			if _, err := c.storage.InsertEvent(c.ctx, evt); err != nil {
				return fmt.Errorf("err insert event: %w", err)
			}
		}

		for _, msg := range transaction.Messages {
			msg.TransactionId = int(txId)
			if _, err := c.storage.InsertMessage(c.ctx, msg); err != nil {
				return fmt.Errorf("err insert message: %w", err)
			}
		}
	}

	return nil
}