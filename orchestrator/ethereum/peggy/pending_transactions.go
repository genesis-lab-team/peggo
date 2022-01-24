package peggy

import (
	"bytes"
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// PendingTxInput contains the data of a pending transaction and the time we first saw it.
type PendingTxInput struct {
	InputData    hexutil.Bytes
	ReceivedTime time.Time
	Gas          hexutil.Bytes
	GasPrice     hexutil.Bytes
	TxType       string
}

type PendingTxInputList []PendingTxInput

// RPCTransaction represents a transaction that will serialize to the RPC representation of a transaction
type RPCTransaction struct {
	Input    hexutil.Bytes `json:"input"`
	Gas      hexutil.Bytes `json:"gas"`
	GasPrice hexutil.Bytes `json:"gasPrice"`
}

// AddPendingTxInput adds pending submitBatch and updateBatch calls to the Peggy contract to the list of pending
// transactions, any other transaction is ignored.
func (p *PendingTxInputList) AddPendingTxInput(pendingTx *RPCTransaction) string {

	submitBatchMethod := peggyABI.Methods["submitBatch"]
	valsetUpdateMethod := peggyABI.Methods["updateValset"]

	// If it's not a submitBatch or updateValset transaction, ignore it.
	// The first four bytes of the call data for a function call specifies the function to be called.
	// Ref: https://docs.soliditylang.org/en/develop/abi-spec.html#function-selector
	if !bytes.Equal(submitBatchMethod.ID, pendingTx.Input[:4]) &&
		!bytes.Equal(valsetUpdateMethod.ID, pendingTx.Input[:4]) {
		return "111"
	}

	pendingTxType := "updateValset"
	if bytes.Equal(submitBatchMethod.ID, pendingTx.Input[:4]) {
		pendingTxType = "submitBatch"
	}

	pendingTxInput := PendingTxInput{
		InputData:    pendingTx.Input,
		ReceivedTime: time.Now(),
		Gas:          pendingTx.Gas,
		GasPrice:     pendingTx.GasPrice,
		TxType:       pendingTxType,
	}

	*p = append(*p, pendingTxInput)
	// Persisting top 100 pending txs of peggy contract only.
	if len(*p) > 100 {
		(*p)[0] = PendingTxInput{} // to avoid memory leak
		// Dequeue pending tx input
		*p = (*p)[1:]
	}

	return pendingTxType
}

func (s *peggyContract) IsPendingTxInput(txData []byte, pendingTxWaitDuration time.Duration) bool {
	t := time.Now()

	for _, pendingTxInput := range s.pendingTxInputList {
		if bytes.Equal(pendingTxInput.InputData, txData) {
			// If this tx was for too long in the pending list, consider it stale
			return t.Before(pendingTxInput.ReceivedTime.Add(pendingTxWaitDuration))
		}
	}
	return false
}

func (s *peggyContract) MaxGasPrice(pendingTxWaitDuration time.Duration) *big.Int {
	t := time.Now()

	maxGas := big.NewInt(0)
	for _, pendingTxInput := range s.pendingTxInputList {
		if !t.Before(pendingTxInput.ReceivedTime.Add(pendingTxWaitDuration)) {
			// If this tx was for too long in the pending list, consider it stale
			s.logger.Info().Msg("PendingGas: Query is old!")
			continue
		}
		if pendingTxInput.TxType == "submitBatch" {
			s.logger.Info().Msg("PendingGas: Calc pending gas")
			gasPrice := hexutil.MustDecodeBig(hexutil.Encode(pendingTxInput.GasPrice))
			isGasPriceGreater := gasPrice.Cmp(maxGas)
			if isGasPriceGreater == 1 {
				maxGas = gasPrice
			}
		}
	}

	return maxGas
}

func (s *peggyContract) SubscribeToPendingTxs(ctx context.Context, alchemyWebsocketURL string) error {
	args := map[string]interface{}{
		"address": s.peggyAddress.Hex(),
	}

	wsClient, err := rpc.Dial(alchemyWebsocketURL)
	if err != nil {
		s.logger.Fatal().
			AnErr("err", err).
			Str("endpoint", alchemyWebsocketURL).
			Msg("failed to connect to Alchemy websocket")
		return err
	}

	ch := make(chan *RPCTransaction)
	_, err = wsClient.EthSubscribe(ctx, ch, "alchemy_filteredNewFullPendingTransactions", args)
	if err != nil {
		s.logger.Fatal().
			AnErr("err", err).
			Str("endpoint", alchemyWebsocketURL).
			Msg("Failed to subscribe to pending transactions")
		return err
	}

	for {
		select {
		case pendingTransaction := <-ch:
			test111 := s.pendingTxInputList.AddPendingTxInput(pendingTransaction)
			// s.logger.Info().Uint64("Gas", hexutil.MustDecodeUint64(hexutil.Encode(pendingTransaction.Gas))).Uint64("GasPrice", hexutil.MustDecodeUint64(hexutil.Encode(pendingTransaction.GasPrice))).Str("TxType",pendingTransaction.TxType).Msg("Gas in pending Txs test")
			s.logger.Info().Str("TypeTx:", test111)

		case <-ctx.Done():
			return nil
		}
	}
}

func (s *peggyContract) GetPendingTxInputList() *PendingTxInputList {
	return &s.pendingTxInputList
}
