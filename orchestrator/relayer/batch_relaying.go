package relayer

import (
	"context"
	"math/big"
	// "sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/shopspring/decimal"
	"github.com/umee-network/umee/x/peggy/types"
)

type SubmittableBatch struct {
	Batch      *types.OutgoingTxBatch
	Signatures []*types.MsgConfirmBatch
}

// getBatchesAndSignatures retrieves the latest batches from the Cosmos module and then iterates through the signatures
// for each batch, determining if they are ready to submit. It is possible for a batch to not have valid signatures for
// two reasons one is that not enough signatures have been collected yet from the validators two is that the batch is
// old enough that the signatures do not reflect the current validator set on Ethereum. In both the later and the former
// case the correct solution is to wait through timeouts, new signatures, or a later valid batch being submitted old
// batches will always be resolved.
func (s *peggyRelayer) getBatchesAndSignatures(
	ctx context.Context,
	currentValset *types.Valset,
) (map[common.Address][]SubmittableBatch, error) {
	possibleBatches := map[common.Address][]SubmittableBatch{}

	outTxBatches, err := s.cosmosQueryClient.OutgoingTxBatches(ctx, &types.QueryOutgoingTxBatchesRequest{})

	if err != nil {
		s.logger.Err(err).Msg("failed to get latest batches")
		return possibleBatches, err
	} else if outTxBatches == nil {
		s.logger.Info().Msg("no outgoing TX batches found")
		return possibleBatches, nil
	}

	// var batchNonceLast uint64 = 0

	// for _, batch := range outTxBatches.Batches {
	// 	decimals := 6
	// 	profitLimit := decimal.NewFromInt(8)
	// 	totalBatchFees := big.NewInt(0)
	//     for _, tx := range batch.Transactions {
	// 	    totalBatchFees = totalBatchFees.Add(tx.Erc20Fee.Amount.BigInt(), totalBatchFees)
	//     }
	// 	totalBatchFeesDec := decimal.NewFromBigInt(totalBatchFees, -int32(decimals))
	// 	isProfitable := totalBatchFeesDec.GreaterThanOrEqual(profitLimit)

	// 	if !isProfitable {
	// 		s.logger.Info().Float64("BatchFees", totalBatchFeesDec.InexactFloat64()).Msg("Not profitable batch fees too low (NH)")
	// 		continue
	// 	}

	// 	if batchNonceLast == 0 || batchNonceLast < batch.BatchNonce {
	// 		batchNonceLast = batch.BatchNonce
	// 	}
	// 	s.logger.Info().Uint64("BatchNonceLast", batchNonceLast).Msg("Profitable!")
	// }

	for _, batch := range outTxBatches.Batches {

		decimals := 6
		profitLimit := decimal.NewFromFloat(0.5)
		totalBatchFees := big.NewInt(0)
	    for _, tx := range batch.Transactions {
		    totalBatchFees = totalBatchFees.Add(tx.Erc20Fee.Amount.BigInt(), totalBatchFees)
	    }
		totalBatchFeesDec := decimal.NewFromBigInt(totalBatchFees, -int32(decimals))
		isProfitable := totalBatchFeesDec.GreaterThanOrEqual(profitLimit)

		if !isProfitable {
			s.logger.Info().Float64("BatchFees", totalBatchFeesDec.InexactFloat64()).Msg("Not profitable batch fees too low (NH)")
			continue
		}
		s.logger.Info().Msg("Profitable but not singer yet!")	

		// if batchNonceLast == 0 {
		// 	s.logger.Info().Msg("(batchNonceLast=0)")
		// 	continue
		// }

		// if batch.BatchNonce != batchNonceLast {
		// 	continue
		// }

		// We might have already sent this same batch. Skip it.
		if s.lastSentBatchNonce >= batch.BatchNonce {
			continue
		}

		batchConfirms, err := s.cosmosQueryClient.BatchConfirms(ctx, &types.QueryBatchConfirmsRequest{
			Nonce:           batch.BatchNonce,
			ContractAddress: batch.TokenContract,
		})

		if err != nil || batchConfirms == nil {
			// If we can't get the signatures for a batch we will continue to the next batch.
			// Use Error() instead of Err() because the latter will print on info level instead of error if err == nil.
			s.logger.Error().
				AnErr("error", err).
				Uint64("batch_nonce", batch.BatchNonce).
				Str("token_contract", batch.TokenContract).
				Msg("failed to get batch's signatures")
			continue
		}

		// This checks that the signatures for the batch are actually possible to submit to the chain.
		// We only need to know if the signatures are good, we won't use the other returned value.
		_, err = s.peggyContract.EncodeTransactionBatch(ctx, currentValset, batch, batchConfirms.Confirms)

		if err != nil {
			// this batch is not ready to be relayed
			s.logger.
				Debug().
				AnErr("err", err).
				Uint64("batch_nonce", batch.BatchNonce).
				Str("token_contract", batch.TokenContract).
				Msg("batch can't be submitted yet, waiting for more signatures")

			// Do not return an error here, we want to continue to the next batch
			continue
		}

		// if batchNonceLast == 0 || batchNonceLast < batch.BatchNonce {
	    // 	batchNonceLast = batch.BatchNonce
		// 	possibleBatches = map[common.Address][]SubmittableBatch{}
		// 	possibleBatches[common.HexToAddress(batch.TokenContract)] = append(
		// 		possibleBatches[common.HexToAddress(batch.TokenContract)],
		// 		SubmittableBatch{Batch: batch, Signatures: batchConfirms.Confirms},
		// 	)
		// 	s.logger.Info().Int("possibleBatches", len(possibleBatches)).Msg(" Possible batches count")
		// 	s.logger.Info().Uint64("BatchNonceLast", batchNonceLast).Msg("Profitable!")	
	    // }

		s.logger.Info().Msg("Profitable and singer!")	
		// if the previous check didn't fail, we can add the batch to the list of possible batches
		possibleBatches[common.HexToAddress(batch.TokenContract)] = append(
			possibleBatches[common.HexToAddress(batch.TokenContract)],
			SubmittableBatch{Batch: batch, Signatures: batchConfirms.Confirms},
		)

		break
	}

	// Order batches by nonce ASC. That means that the next/oldest batch is [0].
	// for tokenAddress := range possibleBatches {
	// 	tokenAddress := tokenAddress
	// 	sort.SliceStable(possibleBatches[tokenAddress], func(i, j int) bool {
	// 		return possibleBatches[tokenAddress][i].Batch.BatchNonce > possibleBatches[tokenAddress][j].Batch.BatchNonce
	// 	})
	// }

	return possibleBatches, nil
}

// RelayBatches attempts to submit batches with valid signatures, checking the state of the Ethereum chain to ensure
// that it is valid to submit a given batch, more specifically that the correctly signed batch has not timed out or
// already been submitted. The goal of this function is to submit batches in chronological order of their creation.
// This function estimates the cost of submitting a batch before submitting it to Ethereum, if it is determined that
// the ETH cost to submit is too high the batch will be skipped and a later, more profitable, batch may be submitted.
// Keep in mind that many other relayers are making this same computation and some may have different standards for
// their profit margin, therefore there may be a race not only to submit individual batches but also batches in
// different orders.
func (s *peggyRelayer) RelayBatches(
	ctx context.Context,
	currentValset *types.Valset,
	possibleBatches map[common.Address][]SubmittableBatch,
) error {
	// first get current block height to check for any timeouts
	lastEthereumHeader, err := s.ethProvider.HeaderByNumber(ctx, nil)
	if err != nil {
		s.logger.Err(err).Msg("failed to get last ethereum header")
		return err
	}

	ethBlockHeight := lastEthereumHeader.Number.Uint64()

	//for tokenContract, batches := range possibleBatches {
	for tokenContract, batches := range possibleBatches {

		s.logger.Info().Int("batches", len(batches)).Msg("Batches count")
		// startPossibleBatchesLoop := time.Now()

		// Requests data from Ethereum only once per token type, this is valid because we are
		// iterating from oldest to newest, so submitting a batch earlier in the loop won't
		// ever invalidate submitting a batch later in the loop. Another relayer could always
		// do that though.
		latestEthereumBatch, err := s.peggyContract.GetTxBatchNonce(
			ctx,
			tokenContract,
			s.peggyContract.FromAddress(),
		)
		if err != nil {
			s.logger.Err(err).Msg("failed to get latest Ethereum batch")
			return err
		}

		// now we iterate through batches per token type
		for _, batch := range batches {
			startBatch := time.Now()

			if batch.Batch.BatchTimeout < ethBlockHeight {
				s.logger.Debug().
					Uint64("batch_nonce", batch.Batch.BatchNonce).
					Str("token_contract", batch.Batch.TokenContract).
					Uint64("batch_timeout", batch.Batch.BatchTimeout).
					Uint64("eth_block_height", ethBlockHeight).
					Msg("batch has timed out and can't be submitted")
				continue
			}

			// if the batch is newer than the latest Ethereum batch, we can submit it
			if batch.Batch.BatchNonce <= latestEthereumBatch.Uint64() {
				s.logger.Info().Msg("Batch is old")
				continue
			}

			txData, err := s.peggyContract.EncodeTransactionBatch(ctx, currentValset, batch.Batch, batch.Signatures)
			if err != nil {
				return err
			}

			// estimatedGasCost, gasPrice, err := s.peggyContract.EstimateGas(ctx, s.peggyContract.Address(), txData)
			// if err != nil {
			// 	s.logger.Err(err).Msg("failed to estimate gas cost")
			// 	return err
			// }

			transactionsInBatch := len(batch.Batch.Transactions)
			totalBatchFees := big.NewInt(0)
	        for _, tx := range batch.Batch.Transactions {
		        totalBatchFees = totalBatchFees.Add(tx.Erc20Fee.Amount.BigInt(), totalBatchFees)
	        }

			estimatedGasCostCalc := transactionsInBatch * 7000 + 530000
			estimatedGasCostDec := decimal.NewFromInt(int64(estimatedGasCostCalc)).Mul(decimal.NewFromFloat(1.1))
			estimatedGasCost := uint64(estimatedGasCostDec.IntPart())


			decimals := 6
			// profitLimit := decimal.NewFromInt(1)
			// profitLimit2 := decimal.NewFromInt(10)
			// umeePrice := decimal.NewFromFloat(0.07)
			// totalBatchFeesUSD := decimal.NewFromBigInt(totalBatchFees, -int32(decimals)).Mul(umeePrice)
			coff := decimal.NewFromFloat(0.000021461752865)
			totalFeeETH := decimal.NewFromBigInt(totalBatchFees, -int32(decimals)).Mul(coff)
			totalGas := totalFeeETH.Div(decimal.NewFromInt(int64(estimatedGasCost)))
			gas := totalGas.Mul(decimal.NewFromInt(1000000000000000000))
			gasPrice := gas.BigInt()

			// isProfitable := totalBatchFeesUSD.GreaterThanOrEqual(profitLimit)
			// isProfitable2 := totalBatchFeesUSD.GreaterThanOrEqual(profitLimit2)


			// totalGas := totalBatchFees * 0.99
			// totalGasUsd := totalGas * 0.07
			// totalGasETH := totalGasUsd / 3229
			// gasPrice := totalGasETH / estimatedGasCost

			// var estimatedGasCost uint64 = 1500000
			// gasPrice := big.NewInt(1700000000)
			
			gP := decimal.NewFromBigInt(gasPrice, -18)
			// gP2 := decimal.NewFromBigInt(gasPriceTest, -18)
			//gP := totalGasUSD

			durationBatch1 := time.Since(startBatch)
			s.logger.Info().Float64("GasPrice", gP.InexactFloat64()).Uint64("GasCost", estimatedGasCost).Int64("BatchTime", durationBatch1.Nanoseconds()).Msg("Below check profit")

			// durationBatch1 := time.Since(startBatch)
			// s.logger.Info().Int64("BatchTime", durationBatch1.Nanoseconds()).Msg("Below check profit")

			// if !isProfitable {
			// 	durationBatch := time.Since(startBatch)
			// 	s.logger.Info().Int64("BatchTime", durationBatch.Nanoseconds()).Msg("Unprofitable")
			// 	continue
			// }

			// if isProfitable2 {
			// 	durationBatch := time.Since(startBatch)
			// 	s.logger.Info().Int64("BatchTime", durationBatch.Nanoseconds()).Msg("Big batch")
			// 	continue
			// }

			// If the batch is not profitable, move on to the next one.
			// if !s.IsBatchProfitable(ctx, batch.Batch, estimatedGasCost, gasPrice, s.profitMultiplier) {
			// 	durationBatch := time.Since(startBatch)
			// 	s.logger.Info().Int64("BatchTime", durationBatch.Nanoseconds()).Msg("Unprofitable")
			// 	continue
			// }

			// Checking in pending txs(mempool) if tx with same input is already submitted
			// We have to check this at the last moment because any other relayer could have submitted.
			// if s.peggyContract.IsPendingTxInput(txData, s.pendingTxWait) {
			// 	s.logger.Error().
			// 		Msg("Transaction with same batch input data is already present in mempool")
			// 		durationBatch := time.Since(startBatch)
			// 		s.logger.Info().Int64("BatchTime", durationBatch.Nanoseconds()).Msg("Profitable, alchemy is not ok")
			// 	continue
			// }

			// s.logger.Info().
			// 	Uint64("latest_batch", batch.Batch.BatchNonce).
			// 	Uint64("latest_ethereum_batch", latestEthereumBatch.Uint64()).
			// 	Msg("we have detected a newer profitable batch; sending an update")

			// isTestGasGreater := gasPriceTest.Cmp(gasPrice)
			// if isTestGasGreater == 1 {
			// 	gasPrice = gasPriceTest
			// }

			gP3 := decimal.NewFromBigInt(gasPrice, -18)
			durationBatch := time.Since(startBatch)
			s.logger.Info().Float64("GasPrice", gP3.InexactFloat64()).Int64("BatchTime", durationBatch.Nanoseconds()).Msg("Profitable, alchemy is ok")

			txHash, err := s.peggyContract.SendTx(ctx, s.peggyContract.Address(), txData, estimatedGasCost, gasPrice)
			if err != nil {
				s.logger.Err(err).Str("tx_hash", txHash.Hex()).Msg("failed to sign and submit (Peggy submitBatch) to EVM")
				return err
			}

			s.logger.Info().Str("tx_hash", txHash.Hex()).Msg("sent Tx (Peggy submitBatch)")

			// update our local tracker of the latest batch
			s.lastSentBatchNonce = batch.Batch.BatchNonce
		}

		// durationPossibleBatchesLoop := time.Since(startPossibleBatchesLoop)
		// s.logger.Info().Int64("PossibleBatchesLoopTime", durationPossibleBatchesLoop.Nanoseconds()).Msg("Possible Batches Loop Time")
	}

	return nil
}

// IsBatchProfitable gets the current prices in USD of ETH and the ERC20 token and compares the value of the estimated
// gas cost of the transaction to the fees paid by the batch. If the estimated gas cost is greater than the batch's
// fees, the batch is not profitable and should not be submitted.
func (s *peggyRelayer) IsBatchProfitable(
	ctx context.Context,
	batch *types.OutgoingTxBatch,
	ethGasCost uint64,
	gasPrice *big.Int,
	profitMultiplier float64,
) bool {
	if s.priceFeeder == nil || profitMultiplier == 0 {
		return true
	}

	// First we get the cost of the transaction in USD
	usdEthPrice := 500.0
	usdEthPriceDec := decimal.NewFromFloat(usdEthPrice)
	totalETHcost := big.NewInt(0).Mul(gasPrice, big.NewInt(int64(ethGasCost)))

	// Ethereum decimals are 18 and that's a constant.
	gasCostInUSDDec := decimal.NewFromBigInt(totalETHcost, -18).Mul(usdEthPriceDec)

	// Then we get the fees of the batch in USD
	decimals := 6

	// s.logger.Debug().
	// 	Uint8("decimals", decimals).
	// 	Str("token_contract", batch.TokenContract).
	// 	Msg("got token decimals")

	// usdTokenPrice, err := s.priceFeeder.QueryUSDPrice(common.HexToAddress(batch.TokenContract))
	// if err != nil {
	// 	return false
	// }

	usdTokenPrice := 1.4

	// We calculate the total fee in ERC20 tokens
	totalBatchFees := big.NewInt(0)
	for _, tx := range batch.Transactions {
		totalBatchFees = totalBatchFees.Add(tx.Erc20Fee.Amount.BigInt(), totalBatchFees)
	}

	usdTokenPriceDec := decimal.NewFromFloat(usdTokenPrice)
	// Decimals (uint8) can be safely casted into int32 because the max uint8 is 255 and the max int32 is 2147483647.
	totalFeeInUSDDec := decimal.NewFromBigInt(totalBatchFees, -int32(decimals)).Mul(usdTokenPriceDec)

	// Simplified: totalFee > (gasCost * profitMultiplier).
	isProfitable := totalFeeInUSDDec.GreaterThanOrEqual(gasCostInUSDDec.Mul(decimal.NewFromFloat(profitMultiplier)))

	s.logger.Debug().
		Str("token_contract", batch.TokenContract).
		Float64("token_price_in_usd", usdTokenPrice).
		Int64("total_fees", totalBatchFees.Int64()).
		Float64("total_fee_in_usd", totalFeeInUSDDec.InexactFloat64()).
		Float64("gas_cost_in_usd", gasCostInUSDDec.InexactFloat64()).
		Float64("profit_multiplier", profitMultiplier).
		Bool("is_profitable", isProfitable).
		Msg("checking if batch is profitable")

	return isProfitable

}
