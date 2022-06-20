package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/hermeznetwork/hermez-core/encoding"
	"github.com/hermeznetwork/hermez-core/hex"
	"github.com/hermeznetwork/hermez-core/state"
	"github.com/hermeznetwork/hermez-core/state/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBlockNumber(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	testCases := []struct {
		name                string
		blockNumber         uint64
		error               error
		expectedBlockNumber uint64
	}{
		{"block number is zero", 0, nil, 0},
		{"block number is not zero", 5, nil, 5},
		{"failed to get block number", 5, errors.New("failed to get block number"), 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			m.State.
				On("GetLastBatchNumber", context.Background(), "").
				Return(testCase.blockNumber, testCase.error).
				Once()

			bn, err := c.BlockNumber(context.Background())
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedBlockNumber, bn)
		})
	}
}

func TestCall(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	type testCase struct {
		name           string
		from           common.Address
		to             *common.Address
		gas            uint64
		gasPrice       *big.Int
		value          *big.Int
		data           []byte
		expectedResult []byte
		expectedError  interface{}
		setupMocks     func(*mocks, *testCase)
	}

	testCases := []*testCase{
		{
			name:           "Transaction with all information",
			from:           common.HexToAddress("0x1"),
			to:             addressPtr(common.HexToAddress("0x2")),
			gas:            uint64(24000),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: []byte("hello world"),
			expectedError:  nil,
			setupMocks: func(m *mocks, testCase *testCase) {
				batchNumber := uint64(1)
				txBundleID := ""
				batch := &state.Batch{Header: &types.Header{Root: common.Hash{}, GasLimit: 123456}}
				m.State.On("GetLastBatchNumber", context.Background(), txBundleID).Return(batchNumber, nil).Once()
				m.State.On("GetBatchByNumber", context.Background(), batchNumber, txBundleID).Return(batch, nil).Once()
				m.State.On("NewBatchProcessor", context.Background(), s.SequencerAddress, batch.Header.Root[:], txBundleID).Return(m.BatchProcessor, nil).Once()
				txMatchBy := mock.MatchedBy(func(tx *types.Transaction) bool {
					return tx != nil &&
						tx.Gas() == testCase.gas &&
						tx.To().Hex() == testCase.to.Hex() &&
						tx.GasPrice().Uint64() == testCase.gasPrice.Uint64() &&
						tx.Value().Uint64() == testCase.value.Uint64() &&
						hex.EncodeToHex(tx.Data()) == hex.EncodeToHex(testCase.data)
				})
				m.BatchProcessor.On("ProcessUnsignedTransaction", context.Background(), txMatchBy, testCase.from, s.SequencerAddress).Return(&runtime.ExecutionResult{ReturnValue: testCase.expectedResult}).Once()
			},
		},
		{
			name:           "Transaction without from and gas",
			to:             addressPtr(common.HexToAddress("0x2")),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: []byte("hello world"),
			expectedError:  nil,
			setupMocks: func(m *mocks, testCase *testCase) {
				batchNumber := uint64(1)
				txBundleID := ""
				batch := &state.Batch{Header: &types.Header{Root: common.Hash{}, GasLimit: 123456}}
				m.State.On("GetLastBatchNumber", context.Background(), txBundleID).Return(batchNumber, nil).Once()
				m.State.On("GetBatchByNumber", context.Background(), batchNumber, txBundleID).Return(batch, nil).Once()
				m.State.On("NewBatchProcessor", context.Background(), s.SequencerAddress, batch.Header.Root[:], txBundleID).Return(m.BatchProcessor, nil).Once()
				txMatchBy := mock.MatchedBy(func(tx *types.Transaction) bool {
					return tx != nil &&
						tx.Gas() == batch.Header.GasLimit &&
						tx.To().Hex() == testCase.to.Hex() &&
						tx.GasPrice().Uint64() == testCase.gasPrice.Uint64() &&
						tx.Value().Uint64() == testCase.value.Uint64() &&
						hex.EncodeToHex(tx.Data()) == hex.EncodeToHex(testCase.data)
				})
				m.BatchProcessor.On("ProcessUnsignedTransaction", context.Background(), txMatchBy, testCase.from, s.SequencerAddress).Return(&runtime.ExecutionResult{ReturnValue: testCase.expectedResult}).Once()
				m.State.On("GetLastBatch", context.Background(), false, txBundleID).Return(batch, nil).Once()
			},
		},
		{
			name:           "Transaction without from and gas and failed to get last batch",
			to:             addressPtr(common.HexToAddress("0x2")),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: nil,
			expectedError:  newRPCError(defaultErrorCode, "failed to get header from block hash or block number"),
			setupMocks: func(m *mocks, testCase *testCase) {
				txBundleID := ""
				m.State.On("GetLastBatch", context.Background(), false, txBundleID).Return(nil, errors.New("failed to get last batch")).Once()
			},
		},
		{
			name:           "Transaction with gas but failed to get last batch number",
			from:           common.HexToAddress("0x1"),
			to:             addressPtr(common.HexToAddress("0x2")),
			gas:            uint64(24000),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: nil,
			expectedError:  newRPCError(defaultErrorCode, "failed to get the last batch number from state"),
			setupMocks: func(m *mocks, testCase *testCase) {
				txBundleID := ""
				m.State.On("GetLastBatchNumber", context.Background(), txBundleID).Return(uint64(0), errors.New("failed to get last batch number")).Once()
			},
		},
		{
			name:           "Transaction with all information but failed to get batch by number",
			from:           common.HexToAddress("0x1"),
			to:             addressPtr(common.HexToAddress("0x2")),
			gas:            uint64(24000),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: nil,
			expectedError:  newRPCError(defaultErrorCode, "failed to get batch by number: 1"),
			setupMocks: func(m *mocks, testCase *testCase) {
				batchNumber := uint64(1)
				txBundleID := ""
				m.State.On("GetLastBatchNumber", context.Background(), txBundleID).Return(batchNumber, nil).Once()
				m.State.On("GetBatchByNumber", context.Background(), batchNumber, txBundleID).Return(nil, errors.New("failed to get batch by number")).Once()
			},
		},
		{
			name:           "Transaction with all information but failed to create batch processor",
			from:           common.HexToAddress("0x1"),
			to:             addressPtr(common.HexToAddress("0x2")),
			gas:            uint64(24000),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: nil,
			expectedError:  newRPCError(defaultErrorCode, "failed to load batch processor"),
			setupMocks: func(m *mocks, testCase *testCase) {
				batchNumber := uint64(1)
				txBundleID := ""
				batch := &state.Batch{Header: &types.Header{Root: common.Hash{}, GasLimit: 123456}}
				m.State.On("GetLastBatchNumber", context.Background(), txBundleID).Return(batchNumber, nil).Once()
				m.State.On("GetBatchByNumber", context.Background(), batchNumber, txBundleID).Return(batch, nil).Once()
				m.State.On("NewBatchProcessor", context.Background(), s.SequencerAddress, batch.Header.Root[:], txBundleID).Return(nil, errors.New("failed to create batch processor")).Once()
			},
		},
		{
			name:           "Transaction with all information but failed to process unsigned transaction",
			from:           common.HexToAddress("0x1"),
			to:             addressPtr(common.HexToAddress("0x2")),
			gas:            uint64(24000),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: nil,
			expectedError:  newRPCError(defaultErrorCode, "failed to execute call: failed to process unsigned transaction"),
			setupMocks: func(m *mocks, testCase *testCase) {
				batchNumber := uint64(1)
				txBundleID := ""
				batch := &state.Batch{Header: &types.Header{Root: common.Hash{}, GasLimit: 123456}}
				m.State.On("GetLastBatchNumber", context.Background(), txBundleID).Return(batchNumber, nil).Once()
				m.State.On("GetBatchByNumber", context.Background(), batchNumber, txBundleID).Return(batch, nil).Once()
				m.State.On("NewBatchProcessor", context.Background(), s.SequencerAddress, batch.Header.Root[:], txBundleID).Return(m.BatchProcessor, nil).Once()
				txMatchBy := mock.MatchedBy(func(tx *types.Transaction) bool {
					return tx != nil &&
						tx.Gas() == testCase.gas &&
						tx.To().Hex() == testCase.to.Hex() &&
						tx.GasPrice().Uint64() == testCase.gasPrice.Uint64() &&
						tx.Value().Uint64() == testCase.value.Uint64() &&
						hex.EncodeToHex(tx.Data()) == hex.EncodeToHex(testCase.data)
				})
				m.BatchProcessor.On("ProcessUnsignedTransaction", context.Background(), txMatchBy, testCase.from, s.SequencerAddress).Return(&runtime.ExecutionResult{Err: errors.New("failed to process unsigned transaction")}).Once()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			msg := ethereum.CallMsg{From: testCase.from, To: testCase.to, Gas: testCase.gas, GasPrice: testCase.gasPrice, Value: testCase.value, Data: testCase.data}

			testCase.setupMocks(m, testCase)

			result, err := c.CallContract(context.Background(), msg, nil)
			assert.Equal(t, testCase.expectedResult, result)
			if err != nil || testCase.expectedError != nil {
				if expectedErr, ok := testCase.expectedError.(*RPCError); ok {
					rpcErr := err.(rpcError)
					assert.Equal(t, expectedErr.ErrorCode(), rpcErr.ErrorCode())
					assert.Equal(t, expectedErr.Error(), rpcErr.Error())
				} else {
					assert.Equal(t, testCase.expectedError, err)
				}
			}
		})
	}
}

func TestChainID(t *testing.T) {
	s, _, c := newMockedServer(t)
	defer s.Stop()

	chainID, err := c.ChainID(context.Background())
	require.NoError(t, err)

	assert.Equal(t, s.ChainID, chainID.Uint64())
}

func TestEstimateGas(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	testCases := []struct {
		name     string
		from     common.Address
		to       *common.Address
		gas      uint64
		gasPrice *big.Int
		value    *big.Int
		data     []byte

		expectedResult uint64
	}{
		{
			name:           "Transaction with all information",
			from:           common.HexToAddress("0x1"),
			to:             addressPtr(common.HexToAddress("0x2")),
			gas:            uint64(24000),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: 100,
		},
		{
			name:           "Transaction without from and gas",
			to:             addressPtr(common.HexToAddress("0x2")),
			gasPrice:       big.NewInt(1),
			value:          big.NewInt(2),
			data:           []byte("data"),
			expectedResult: 100,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			msg := ethereum.CallMsg{From: testCase.from, To: testCase.to, Gas: testCase.gas, GasPrice: testCase.gasPrice, Value: testCase.value, Data: testCase.data}

			txMatchBy := mock.MatchedBy(func(tx *types.Transaction) bool {
				if tx == nil {
					return false
				}

				return tx.To().Hex() == testCase.to.Hex() &&
					tx.GasPrice().Uint64() == testCase.gasPrice.Uint64() &&
					tx.Value().Uint64() == testCase.value.Uint64() &&
					hex.EncodeToHex(tx.Data()) == hex.EncodeToHex(testCase.data)
			})

			m.State.
				On("EstimateGas", txMatchBy, testCase.from).
				Return(testCase.expectedResult, nil).
				Once()

			result, err := c.EstimateGas(context.Background(), msg)
			require.NoError(t, err)

			assert.Equal(t, testCase.expectedResult, result)
		})
	}
}

func TestGasPrice(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	testCases := []struct {
		name             string
		gasPrice         *big.Int
		error            error
		expectedGasPrice uint64
	}{
		{"GasPrice nil", nil, nil, 0},
		{"GasPrice with value", big.NewInt(50), nil, 50},
		{"failed to get gas price", big.NewInt(50), errors.New("failed to get gas price"), 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			m.GasPriceEstimator.
				On("GetAvgGasPrice", context.Background()).
				Return(testCase.gasPrice, testCase.error).
				Once()

			gasPrice, err := c.SuggestGasPrice(context.Background())
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedGasPrice, gasPrice.Uint64())
		})
	}
}

func TestGetBalance(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	type testCase struct {
		name            string
		addr            common.Address
		balance         *big.Int
		blockNumber     *big.Int
		expectedBalance uint64
		expectedError   *RPCError
		setupMocks      func(m *mocks, t *testCase)
	}

	testCases := []testCase{
		{
			name:            "get balance but failed to get latest block number",
			addr:            common.HexToAddress("0x123"),
			balance:         big.NewInt(1000),
			blockNumber:     nil,
			expectedBalance: 0,
			expectedError:   newRPCError(defaultErrorCode, "failed to get the last batch number from state"),
			setupMocks: func(m *mocks, t *testCase) {
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(uint64(0), errors.New("failed to get last batch number")).
					Once()
			},
		},
		{
			name:            "get balance for block nil",
			addr:            common.HexToAddress("0x123"),
			balance:         big.NewInt(1000),
			blockNumber:     nil,
			expectedBalance: 1000,
			expectedError:   nil,
			setupMocks: func(m *mocks, t *testCase) {
				const lastBlockNumber = uint64(10)
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(lastBlockNumber, nil).
					Once()

				m.State.
					On("GetBalance", context.Background(), t.addr, lastBlockNumber, "").
					Return(t.balance, nil).
					Once()
			},
		},
		{
			name:            "get balance for not found result",
			addr:            common.HexToAddress("0x123"),
			balance:         big.NewInt(1000),
			blockNumber:     nil,
			expectedBalance: 0,
			expectedError:   nil,
			setupMocks: func(m *mocks, t *testCase) {
				const lastBlockNumber = uint64(10)
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(lastBlockNumber, nil).
					Once()

				m.State.
					On("GetBalance", context.Background(), t.addr, lastBlockNumber, "").
					Return(big.NewInt(0), state.ErrNotFound).
					Once()
			},
		},
		{
			name:            "get balance with state failure",
			addr:            common.HexToAddress("0x123"),
			balance:         big.NewInt(1000),
			blockNumber:     nil,
			expectedBalance: 0,
			expectedError:   newRPCError(defaultErrorCode, "failed to get balance from state"),
			setupMocks: func(m *mocks, t *testCase) {
				const lastBlockNumber = uint64(10)
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(lastBlockNumber, nil).
					Once()

				m.State.
					On("GetBalance", context.Background(), t.addr, lastBlockNumber, "").
					Return(nil, errors.New("failed to get balance")).
					Once()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc := testCase
			testCase.setupMocks(m, &tc)
			balance, err := c.BalanceAt(context.Background(), tc.addr, tc.blockNumber)
			assert.Equal(t, tc.expectedBalance, balance.Uint64())
			if err != nil || tc.expectedError != nil {
				rpcErr := err.(rpcError)
				assert.Equal(t, tc.expectedError.ErrorCode(), rpcErr.ErrorCode())
				assert.Equal(t, tc.expectedError.Error(), rpcErr.Error())
			}
		})
	}
}

func TestGetBlockByHash(t *testing.T) {
	type testCase struct {
		Name           string
		Hash           common.Hash
		ExpectedResult *types.Block
		ExpectedError  interface{}
		SetupMocks     func(*mocks, *testCase)
	}

	testCases := []testCase{
		{
			Name:           "Block not found",
			Hash:           common.HexToHash("0x123"),
			ExpectedResult: nil,
			ExpectedError:  ethereum.NotFound,
			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetBatchByHash", context.Background(), tc.Hash, "").
					Return(nil, state.ErrNotFound)
			},
		},
		{
			Name:           "Failed get block from state",
			Hash:           common.HexToHash("0x234"),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get block from state"),
			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetBatchByHash", context.Background(), tc.Hash, "").
					Return(nil, errors.New("failed to get block from state")).
					Once()
			},
		},
		{
			Name: "get block successfully",
			Hash: common.HexToHash("0x345"),
			ExpectedResult: types.NewBlock(
				&types.Header{Number: big.NewInt(1), UncleHash: types.EmptyUncleHash, Root: types.EmptyRootHash},
				[]*types.Transaction{types.NewTransaction(1, common.Address{}, big.NewInt(1), 1, big.NewInt(1), []byte{})},
				nil,
				[]*types.Receipt{types.NewReceipt([]byte{}, false, uint64(0))},
				&trie.StackTrie{},
			),
			ExpectedError: nil,
			SetupMocks: func(m *mocks, tc *testCase) {
				transactions := []*types.Transaction{}
				for _, tx := range tc.ExpectedResult.Transactions() {
					transactions = append(transactions, tx)
				}

				batch := &state.Batch{
					Header:       types.CopyHeader(tc.ExpectedResult.Header()),
					Transactions: transactions,
				}

				m.State.
					On("GetBatchByHash", context.Background(), tc.Hash, "").
					Return(batch, nil).
					Once()
			},
		},
	}

	s, m, c := newMockedServer(t)
	defer s.Stop()

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			tc := testCase
			testCase.SetupMocks(m, &tc)

			result, err := c.BlockByHash(context.Background(), tc.Hash)

			if result != nil || tc.ExpectedResult != nil {
				assert.Equal(t, tc.ExpectedResult.Number().Uint64(), result.Number().Uint64())
				assert.Equal(t, len(tc.ExpectedResult.Transactions()), len(result.Transactions()))
				assert.Equal(t, tc.ExpectedResult.Hash(), result.Hash())
			}

			if err != nil || tc.ExpectedError != nil {
				if expectedErr, ok := tc.ExpectedError.(*RPCError); ok {
					rpcErr := err.(rpcError)
					assert.Equal(t, expectedErr.ErrorCode(), rpcErr.ErrorCode())
					assert.Equal(t, expectedErr.Error(), rpcErr.Error())
				} else {
					assert.Equal(t, tc.ExpectedError, err)
				}
			}
		})
	}
}

func TestGetBlockByNumber(t *testing.T) {
	type testCase struct {
		Name           string
		Number         *big.Int
		ExpectedResult *types.Block
		ExpectedError  interface{}
		SetupMocks     func(*mocks, *testCase)
	}

	testCases := []testCase{
		{
			Name:           "Block not found",
			Number:         big.NewInt(123),
			ExpectedResult: nil,
			ExpectedError:  ethereum.NotFound,
			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetBatchByNumber", context.Background(), tc.Number.Uint64(), "").
					Return(nil, state.ErrNotFound)
			},
		},
		{
			Name:   "get specific block successfully",
			Number: big.NewInt(345),
			ExpectedResult: types.NewBlock(
				&types.Header{Number: big.NewInt(1), UncleHash: types.EmptyUncleHash, Root: types.EmptyRootHash},
				[]*types.Transaction{types.NewTransaction(1, common.Address{}, big.NewInt(1), 1, big.NewInt(1), []byte{})},
				nil,
				[]*types.Receipt{types.NewReceipt([]byte{}, false, uint64(0))},
				&trie.StackTrie{},
			),
			ExpectedError: nil,
			SetupMocks: func(m *mocks, tc *testCase) {
				transactions := []*types.Transaction{}
				for _, tx := range tc.ExpectedResult.Transactions() {
					transactions = append(transactions, tx)
				}

				batch := &state.Batch{
					Header:       types.CopyHeader(tc.ExpectedResult.Header()),
					Transactions: transactions,
				}

				m.State.
					On("GetBatchByNumber", context.Background(), tc.Number.Uint64(), "").
					Return(batch, nil).
					Once()
			},
		},
		{
			Name:   "get latest block successfully",
			Number: nil,
			ExpectedResult: types.NewBlock(
				&types.Header{Number: big.NewInt(2), UncleHash: types.EmptyUncleHash, Root: types.EmptyRootHash},
				[]*types.Transaction{types.NewTransaction(1, common.Address{}, big.NewInt(1), 1, big.NewInt(1), []byte{})},
				nil,
				[]*types.Receipt{types.NewReceipt([]byte{}, false, uint64(0))},
				&trie.StackTrie{},
			),
			ExpectedError: nil,
			SetupMocks: func(m *mocks, tc *testCase) {
				transactions := []*types.Transaction{}
				for _, tx := range tc.ExpectedResult.Transactions() {
					transactions = append(transactions, tx)
				}

				batch := &state.Batch{
					Header:       types.CopyHeader(tc.ExpectedResult.Header()),
					Transactions: transactions,
				}

				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(batch.Number().Uint64(), nil).
					Once()

				m.State.
					On("GetBatchByNumber", context.Background(), batch.Number().Uint64(), "").
					Return(batch, nil).
					Once()
			},
		},
		{
			Name:           "get latest block fails to compute block number",
			Number:         nil,
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get the last batch number from state"),
			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(uint64(0), errors.New("failed to get last batch number")).
					Once()
			},
		},
		{
			Name:           "get latest block fails to load batch by number",
			Number:         nil,
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "couldn't load batch from state by number 1"),
			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(uint64(1), nil).
					Once()

				m.State.
					On("GetBatchByNumber", context.Background(), uint64(1), "").
					Return(nil, errors.New("failed to load batch by number")).
					Once()
			},
		},
		{
			Name:           "get pending block successfully",
			Number:         big.NewInt(-1),
			ExpectedResult: types.NewBlock(&types.Header{Number: big.NewInt(2)}, nil, nil, nil, &trie.StackTrie{}),
			ExpectedError:  nil,
			SetupMocks: func(m *mocks, tc *testCase) {
				transactions := []*types.Transaction{}
				for _, tx := range tc.ExpectedResult.Transactions() {
					transactions = append(transactions, tx)
				}

				lastBatch := &state.Batch{
					Header:       types.CopyHeader(tc.ExpectedResult.Header()),
					Transactions: transactions,
				}

				lastBatch.Header.Number.Sub(lastBatch.Header.Number, big.NewInt(1))

				expectedResultHeader := types.CopyHeader(tc.ExpectedResult.Header())
				expectedResultHeader.ParentHash = lastBatch.Hash()
				tc.ExpectedResult = types.NewBlock(expectedResultHeader, nil, nil, nil, &trie.StackTrie{})

				m.State.
					On("GetLastBatch", context.Background(), true, "").
					Return(lastBatch, nil).
					Once()
			},
		},
		{
			Name:           "get pending block fails",
			Number:         big.NewInt(-1),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "couldn't load last batch from state to compute the pending block"),
			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetLastBatch", context.Background(), true, "").
					Return(nil, errors.New("failed to load last batch")).
					Once()
			},
		},
	}

	s, m, c := newMockedServer(t)
	defer s.Stop()

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			tc := testCase
			testCase.SetupMocks(m, &tc)

			result, err := c.BlockByNumber(context.Background(), tc.Number)

			if result != nil || tc.ExpectedResult != nil {
				expectedResultJSON, _ := json.Marshal(tc.ExpectedResult.Header())
				resultJSON, _ := json.Marshal(result.Header())

				expectedResultJSONStr := string(expectedResultJSON)
				resultJSONStr := string(resultJSON)

				assert.JSONEq(t, expectedResultJSONStr, resultJSONStr)
				assert.Equal(t, tc.ExpectedResult.Number().Uint64(), result.Number().Uint64())
				assert.Equal(t, len(tc.ExpectedResult.Transactions()), len(result.Transactions()))
				assert.Equal(t, tc.ExpectedResult.Hash(), result.Hash())
			}

			if err != nil || tc.ExpectedError != nil {
				if expectedErr, ok := tc.ExpectedError.(*RPCError); ok {
					rpcErr := err.(rpcError)
					assert.Equal(t, expectedErr.ErrorCode(), rpcErr.ErrorCode())
					assert.Equal(t, expectedErr.Error(), rpcErr.Error())
				} else {
					assert.Equal(t, tc.ExpectedError, err)
				}
			}
		})
	}
}

func TestGetUncleByBlockHashAndIndex(t *testing.T) {
	s, _, _ := newMockedServer(t)
	defer s.Stop()

	res, err := s.JSONRPCCall("eth_getUncleByBlockHashAndIndex", common.HexToHash("0x123").Hex(), "0x1")
	require.NoError(t, err)

	assert.Equal(t, float64(1), res.ID)
	assert.Equal(t, "2.0", res.JSONRPC)
	assert.Nil(t, res.Error)

	var result interface{}
	err = json.Unmarshal(res.Result, &result)
	require.NoError(t, err)

	assert.Nil(t, result)
}

func TestGetUncleByBlockNumberAndIndex(t *testing.T) {
	s, _, _ := newMockedServer(t)
	defer s.Stop()

	res, err := s.JSONRPCCall("eth_getUncleByBlockNumberAndIndex", "0x123", "0x1")
	require.NoError(t, err)

	assert.Equal(t, float64(1), res.ID)
	assert.Equal(t, "2.0", res.JSONRPC)
	assert.Nil(t, res.Error)

	var result interface{}
	err = json.Unmarshal(res.Result, &result)
	require.NoError(t, err)

	assert.Nil(t, result)
}

func TestGetUncleCountByBlockHash(t *testing.T) {
	s, _, _ := newMockedServer(t)
	defer s.Stop()

	res, err := s.JSONRPCCall("eth_getUncleCountByBlockHash", common.HexToHash("0x123"))
	require.NoError(t, err)

	assert.Equal(t, float64(1), res.ID)
	assert.Equal(t, "2.0", res.JSONRPC)
	assert.Nil(t, res.Error)

	var result argUint64
	err = json.Unmarshal(res.Result, &result)
	require.NoError(t, err)

	assert.Equal(t, uint64(0), uint64(result))
}

func TestGetUncleCountByBlockNumber(t *testing.T) {
	s, _, _ := newMockedServer(t)
	defer s.Stop()

	res, err := s.JSONRPCCall("eth_getUncleCountByBlockNumber", "0x123")
	require.NoError(t, err)

	assert.Equal(t, float64(1), res.ID)
	assert.Equal(t, "2.0", res.JSONRPC)
	assert.Nil(t, res.Error)

	var result argUint64
	err = json.Unmarshal(res.Result, &result)
	require.NoError(t, err)

	assert.Equal(t, uint64(0), uint64(result))
}

func TestGetTransactionReceipt(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	type testCase struct {
		Name string
		Hash common.Hash

		ExpectedResult *types.Receipt
		ExpectedError  error

		SetupMocks func(m *mocks, t testCase)
	}

	testCases := []testCase{
		{
			Name:           "tx receipt not found",
			Hash:           common.HexToHash("0x1"),
			ExpectedResult: nil,
			ExpectedError:  ethereum.NotFound,
			SetupMocks: func(m *mocks, t testCase) {
				m.State.
					On("GetTransactionReceipt", context.Background(), t.Hash, "").
					Return(nil, state.ErrNotFound).
					Once()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			testCase.SetupMocks(m, testCase)
			r, err := c.TransactionReceipt(context.Background(), testCase.Hash)
			assert.Equal(t, testCase.ExpectedResult, r)
			assert.Equal(t, testCase.ExpectedError, err)
		})
	}
}

func TestGetCode(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	type testCase struct {
		Name           string
		Addr           common.Address
		BlockNumber    *big.Int
		ExpectedResult []byte
		ExpectedError  interface{}

		SetupMocks func(m *mocks, tc *testCase)
	}

	testCases := []testCase{
		{
			Name:           "failed to identify the block",
			Addr:           common.HexToAddress("0x123"),
			BlockNumber:    nil,
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get the last batch number from state"),

			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(uint64(0), errors.New("failed to get last batch number")).
					Once()
			},
		},
		{
			Name:           "failed to get code",
			Addr:           common.HexToAddress("0x123"),
			BlockNumber:    big.NewInt(1),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get code"),

			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetCode", context.Background(), tc.Addr, tc.BlockNumber.Uint64(), "").
					Return(nil, errors.New("failed to get code")).
					Once()
			},
		},
		{
			Name:           "code not found",
			Addr:           common.HexToAddress("0x123"),
			BlockNumber:    big.NewInt(1),
			ExpectedResult: []byte{},
			ExpectedError:  nil,

			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetCode", context.Background(), tc.Addr, tc.BlockNumber.Uint64(), "").
					Return(nil, state.ErrNotFound).
					Once()
			},
		},
		{
			Name:           "get code successfully",
			Addr:           common.HexToAddress("0x123"),
			BlockNumber:    big.NewInt(1),
			ExpectedResult: []byte{1, 2, 3},
			ExpectedError:  nil,

			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetCode", context.Background(), tc.Addr, tc.BlockNumber.Uint64(), "").
					Return(tc.ExpectedResult, nil).
					Once()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			tc := testCase
			tc.SetupMocks(m, &tc)
			result, err := c.CodeAt(context.Background(), tc.Addr, tc.BlockNumber)
			assert.Equal(t, tc.ExpectedResult, result)

			if err != nil || tc.ExpectedError != nil {
				if expectedErr, ok := tc.ExpectedError.(*RPCError); ok {
					rpcErr := err.(rpcError)
					assert.Equal(t, expectedErr.ErrorCode(), rpcErr.ErrorCode())
					assert.Equal(t, expectedErr.Error(), rpcErr.Error())
				} else {
					assert.Equal(t, tc.ExpectedError, err)
				}
			}
		})
	}
}

func TestGetStorageAt(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	type testCase struct {
		Name           string
		Addr           common.Address
		Key            common.Hash
		BlockNumber    *big.Int
		ExpectedResult []byte
		ExpectedError  interface{}

		SetupMocks func(m *mocks, tc *testCase)
	}

	testCases := []testCase{
		{
			Name:           "failed to identify the block",
			Addr:           common.HexToAddress("0x123"),
			Key:            common.HexToHash("0x123"),
			BlockNumber:    nil,
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get the last batch number from state"),

			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(uint64(0), errors.New("failed to get last batch number")).
					Once()
			},
		},
		{
			Name:           "failed to get code",
			Addr:           common.HexToAddress("0x123"),
			Key:            common.HexToHash("0x123"),
			BlockNumber:    big.NewInt(1),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get code"),

			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetStorageAt", context.Background(), tc.Addr, tc.Key.Big(), tc.BlockNumber.Uint64(), "").
					Return(nil, errors.New("failed to get code")).
					Once()
			},
		},
		{
			Name:           "code not found",
			Addr:           common.HexToAddress("0x123"),
			Key:            common.HexToHash("0x123"),
			BlockNumber:    big.NewInt(1),
			ExpectedResult: common.Hash{}.Bytes(),
			ExpectedError:  nil,

			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetStorageAt", context.Background(), tc.Addr, tc.Key.Big(), tc.BlockNumber.Uint64(), "").
					Return(nil, state.ErrNotFound).
					Once()
			},
		},
		{
			Name:           "get code successfully",
			Addr:           common.HexToAddress("0x123"),
			Key:            common.HexToHash("0x123"),
			BlockNumber:    big.NewInt(1),
			ExpectedResult: common.BigToHash(big.NewInt(123)).Bytes(),
			ExpectedError:  nil,

			SetupMocks: func(m *mocks, tc *testCase) {
				m.State.
					On("GetStorageAt", context.Background(), tc.Addr, tc.Key.Big(), tc.BlockNumber.Uint64(), "").
					Return(big.NewInt(123), nil).
					Once()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			tc := testCase
			tc.SetupMocks(m, &tc)
			result, err := c.StorageAt(context.Background(), tc.Addr, tc.Key, tc.BlockNumber)
			assert.Equal(t, tc.ExpectedResult, result)

			if err != nil || tc.ExpectedError != nil {
				if expectedErr, ok := tc.ExpectedError.(*RPCError); ok {
					rpcErr := err.(rpcError)
					assert.Equal(t, expectedErr.ErrorCode(), rpcErr.ErrorCode())
					assert.Equal(t, expectedErr.Error(), rpcErr.Error())
				} else {
					assert.Equal(t, tc.ExpectedError, err)
				}
			}
		})
	}
}

func TestGetCompilers(t *testing.T) {
	s, _, _ := newMockedServer(t)
	defer s.Stop()

	res, err := s.JSONRPCCall("eth_getCompilers")
	require.NoError(t, err)

	assert.Equal(t, float64(1), res.ID)
	assert.Equal(t, "2.0", res.JSONRPC)
	assert.Nil(t, res.Error)

	var result []interface{}
	err = json.Unmarshal(res.Result, &result)
	require.NoError(t, err)

	assert.Equal(t, 0, len(result))
}

func TestSyncing(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	type testCase struct {
		Name           string
		ExpectedResult *ethereum.SyncProgress
		ExpectedError  rpcError
		SetupMocks     func(m *mocks, tc testCase)
	}

	testCases := []testCase{
		{
			Name:           "failed to get syncing information",
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get syncing info from state"),
			SetupMocks: func(m *mocks, tc testCase) {
				m.State.
					On("GetSyncingInfo", context.Background(), "").
					Return(state.SyncingInfo{}, errors.New("failed to get syncing info from state")).
					Once()
			},
		},
		{
			Name:           "get syncing information successfully while syncing",
			ExpectedResult: &ethereum.SyncProgress{StartingBlock: 1, CurrentBlock: 2, HighestBlock: 3},
			ExpectedError:  nil,
			SetupMocks: func(m *mocks, tc testCase) {
				m.State.
					On("GetSyncingInfo", context.Background(), "").
					Return(state.SyncingInfo{InitialSyncingBatch: 1, CurrentBatchNumber: 2, LastBatchNumberSeen: 3, LastBatchNumberConsolidated: 3}, nil).
					Once()
			},
		},
		{
			Name:           "get syncing information successfully when synced",
			ExpectedResult: nil,
			ExpectedError:  nil,
			SetupMocks: func(m *mocks, tc testCase) {
				m.State.
					On("GetSyncingInfo", context.Background(), "").
					Return(state.SyncingInfo{InitialSyncingBatch: 1, CurrentBatchNumber: 1, LastBatchNumberSeen: 1, LastBatchNumberConsolidated: 1}, nil).
					Once()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			testCase.SetupMocks(m, testCase)
			result, err := c.SyncProgress(context.Background())

			if result != nil || testCase.ExpectedResult != nil {
				assert.Equal(t, testCase.ExpectedResult.StartingBlock, result.StartingBlock)
				assert.Equal(t, testCase.ExpectedResult.CurrentBlock, result.CurrentBlock)
				assert.Equal(t, testCase.ExpectedResult.HighestBlock, result.HighestBlock)
			}

			if err != nil || testCase.ExpectedError != nil {
				if expectedErr, ok := testCase.ExpectedError.(*RPCError); ok {
					rpcErr := err.(rpcError)
					assert.Equal(t, expectedErr.ErrorCode(), rpcErr.ErrorCode())
					assert.Equal(t, expectedErr.Error(), rpcErr.Error())
				} else {
					assert.Equal(t, testCase.ExpectedError, err)
				}
			}
		})
	}
}

func TestGetTransactionByBlockHashAndIndex(t *testing.T) {
	s, m, c := newMockedServer(t)
	defer s.Stop()

	type testCase struct {
		Name  string
		Hash  common.Hash
		Index uint

		ExpectedResult *types.Transaction
		ExpectedError  interface{}
		SetupMocks     func(m *mocks, tc testCase)
	}

	testCases := []testCase{
		{
			Name:           "Get Tx Successfully",
			Hash:           common.HexToHash("0x999"),
			Index:          uint(1),
			ExpectedResult: types.NewTransaction(1, common.HexToAddress("0x111"), big.NewInt(2), 3, big.NewInt(4), []byte{5, 6, 7, 8}),
			ExpectedError:  nil,
			SetupMocks: func(m *mocks, tc testCase) {
				tx := tc.ExpectedResult

				m.State.
					On("GetTransactionByBatchHashAndIndex", context.Background(), tc.Hash, uint64(tc.Index), "").
					Return(tx, nil).
					Once()

				receipt := types.NewReceipt([]byte{}, false, 0)
				receipt.BlockHash = common.Hash{}
				receipt.BlockNumber = big.NewInt(1)
				receipt.TransactionIndex = tc.Index
				stateReceipt := &state.Receipt{Receipt: *receipt}

				m.State.
					On("GetTransactionReceipt", context.Background(), tx.Hash(), "").
					Return(stateReceipt, nil).
					Once()
			},
		},
		{
			Name:           "Tx not found",
			Hash:           common.HexToHash("0x999"),
			Index:          uint(1),
			ExpectedResult: nil,
			ExpectedError:  ethereum.NotFound,
			SetupMocks: func(m *mocks, tc testCase) {
				m.State.
					On("GetTransactionByBatchHashAndIndex", context.Background(), tc.Hash, uint64(tc.Index), "").
					Return(nil, state.ErrNotFound).
					Once()
			},
		},
		{
			Name:           "Get Tx fail to get tx from state",
			Hash:           common.HexToHash("0x999"),
			Index:          uint(1),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get transaction"),
			SetupMocks: func(m *mocks, tc testCase) {
				m.State.
					On("GetTransactionByBatchHashAndIndex", context.Background(), tc.Hash, uint64(tc.Index), "").
					Return(nil, errors.New("failed to get transaction by batch and index from state")).
					Once()
			},
		},
		{
			Name:           "Tx found but receipt not found",
			Hash:           common.HexToHash("0x999"),
			Index:          uint(1),
			ExpectedResult: nil,
			ExpectedError:  ethereum.NotFound,
			SetupMocks: func(m *mocks, tc testCase) {
				tx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, big.NewInt(0), []byte{})

				m.State.
					On("GetTransactionByBatchHashAndIndex", context.Background(), tc.Hash, uint64(tc.Index), "").
					Return(tx, nil).
					Once()

				m.State.
					On("GetTransactionReceipt", context.Background(), tx.Hash(), "").
					Return(nil, state.ErrNotFound).
					Once()
			},
		},
		{
			Name:           "Get Tx fail to get tx receipt from state",
			Hash:           common.HexToHash("0x999"),
			Index:          uint(1),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get transaction receipt"),
			SetupMocks: func(m *mocks, tc testCase) {
				tx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, big.NewInt(0), []byte{})

				m.State.
					On("GetTransactionByBatchHashAndIndex", context.Background(), tc.Hash, uint64(tc.Index), "").
					Return(tx, nil).
					Once()

				m.State.
					On("GetTransactionReceipt", context.Background(), tx.Hash(), "").
					Return(nil, errors.New("failed to get transaction receipt from state")).
					Once()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			tc := testCase
			tc.SetupMocks(m, tc)

			result, err := c.TransactionInBlock(context.Background(), tc.Hash, tc.Index)

			if result != nil || testCase.ExpectedResult != nil {
				assert.Equal(t, testCase.ExpectedResult.Hash(), result.Hash())
			}

			if err != nil || testCase.ExpectedError != nil {
				if expectedErr, ok := testCase.ExpectedError.(*RPCError); ok {
					rpcErr := err.(rpcError)
					assert.Equal(t, expectedErr.ErrorCode(), rpcErr.ErrorCode())
					assert.Equal(t, expectedErr.Error(), rpcErr.Error())
				} else {
					assert.Equal(t, testCase.ExpectedError, err)
				}
			}
		})
	}
}

func TestGetTransactionByBlockNumberAndIndex(t *testing.T) {
	s, m, _ := newMockedServer(t)
	defer s.Stop()

	type testCase struct {
		Name        string
		BlockNumber string
		Index       uint

		ExpectedResult *types.Transaction
		ExpectedError  rpcError
		SetupMocks     func(m *mocks, tc testCase)
	}

	testCases := []testCase{
		{
			Name:           "Get Tx Successfully",
			BlockNumber:    "0x1",
			Index:          uint(0),
			ExpectedResult: types.NewTransaction(1, common.HexToAddress("0x111"), big.NewInt(2), 3, big.NewInt(4), []byte{5, 6, 7, 8}),
			ExpectedError:  nil,
			SetupMocks: func(m *mocks, tc testCase) {
				tx := tc.ExpectedResult
				blockNumber, _ := encoding.DecodeUint64orHex(&tc.BlockNumber)
				m.State.
					On("GetTransactionByBatchNumberAndIndex", context.Background(), blockNumber, uint64(tc.Index), "").
					Return(tx, nil).
					Once()

				receipt := types.NewReceipt([]byte{}, false, 0)
				receipt.BlockHash = common.Hash{}
				receipt.BlockNumber = big.NewInt(1)
				receipt.TransactionIndex = tc.Index
				stateReceipt := &state.Receipt{Receipt: *receipt}

				m.State.
					On("GetTransactionReceipt", context.Background(), tx.Hash(), "").
					Return(stateReceipt, nil).
					Once()
			},
		},
		{
			Name:           "failed to identify block number",
			BlockNumber:    "latest",
			Index:          uint(0),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get the last batch number from state"),
			SetupMocks: func(m *mocks, tc testCase) {
				m.State.
					On("GetLastBatchNumber", context.Background(), "").
					Return(uint64(0), errors.New("failed to get last batch number")).
					Once()
			},
		},
		{
			Name:           "Tx not found",
			BlockNumber:    "0x1",
			Index:          uint(0),
			ExpectedResult: nil,
			ExpectedError:  nil,
			SetupMocks: func(m *mocks, tc testCase) {
				blockNumber, _ := encoding.DecodeUint64orHex(&tc.BlockNumber)
				m.State.
					On("GetTransactionByBatchNumberAndIndex", context.Background(), blockNumber, uint64(tc.Index), "").
					Return(nil, state.ErrNotFound).
					Once()
			},
		},
		{
			Name:           "Get Tx fail to get tx from state",
			BlockNumber:    "0x1",
			Index:          uint(0),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get transaction"),
			SetupMocks: func(m *mocks, tc testCase) {
				blockNumber, _ := encoding.DecodeUint64orHex(&tc.BlockNumber)
				m.State.
					On("GetTransactionByBatchNumberAndIndex", context.Background(), blockNumber, uint64(tc.Index), "").
					Return(nil, errors.New("failed to get transaction by batch and index from state")).
					Once()
			},
		},
		{
			Name:           "Tx found but receipt not found",
			BlockNumber:    "0x1",
			Index:          uint(0),
			ExpectedResult: nil,
			ExpectedError:  nil,
			SetupMocks: func(m *mocks, tc testCase) {
				tx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, big.NewInt(0), []byte{})

				blockNumber, _ := encoding.DecodeUint64orHex(&tc.BlockNumber)
				m.State.
					On("GetTransactionByBatchNumberAndIndex", context.Background(), blockNumber, uint64(tc.Index), "").
					Return(tx, nil).
					Once()

				m.State.
					On("GetTransactionReceipt", context.Background(), tx.Hash(), "").
					Return(nil, state.ErrNotFound).
					Once()
			},
		},
		{
			Name:           "Get Tx fail to get tx receipt from state",
			BlockNumber:    "0x1",
			Index:          uint(0),
			ExpectedResult: nil,
			ExpectedError:  newRPCError(defaultErrorCode, "failed to get transaction receipt"),
			SetupMocks: func(m *mocks, tc testCase) {
				tx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, big.NewInt(0), []byte{})

				blockNumber, _ := encoding.DecodeUint64orHex(&tc.BlockNumber)
				m.State.
					On("GetTransactionByBatchNumberAndIndex", context.Background(), blockNumber, uint64(tc.Index), "").
					Return(tx, nil).
					Once()

				m.State.
					On("GetTransactionReceipt", context.Background(), tx.Hash(), "").
					Return(nil, errors.New("failed to get transaction receipt from state")).
					Once()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			tc := testCase
			tc.SetupMocks(m, tc)

			res, err := s.JSONRPCCall("eth_getTransactionByBlockNumberAndIndex", tc.BlockNumber, tc.Index)
			require.NoError(t, err)
			assert.Equal(t, float64(1), res.ID)
			assert.Equal(t, "2.0", res.JSONRPC)

			if res.Result != nil {
				var result interface{}
				err = json.Unmarshal(res.Result, &result)
				require.NoError(t, err)

				if result != nil || testCase.ExpectedResult != nil {
					var tx types.Transaction
					err = json.Unmarshal(res.Result, &tx)
					require.NoError(t, err)
					assert.Equal(t, testCase.ExpectedResult.Hash(), tx.Hash())
				}
			}

			if res.Error != nil || testCase.ExpectedError != nil {
				assert.Equal(t, testCase.ExpectedError.ErrorCode(), res.Error.Code)
				assert.Equal(t, testCase.ExpectedError.Error(), res.Error.Message)
			}
		})
	}
}

func TestProtocolVersion(t *testing.T) {
	s, _, _ := newMockedServer(t)
	defer s.Stop()

	res, err := s.JSONRPCCall("eth_protocolVersion")
	require.NoError(t, err)

	assert.Equal(t, float64(1), res.ID)
	assert.Equal(t, "2.0", res.JSONRPC)
	assert.Nil(t, res.Error)

	var result string
	err = json.Unmarshal(res.Result, &result)
	require.NoError(t, err)

	assert.Equal(t, "0x0", result)
}

func addressPtr(i common.Address) *common.Address {
	return &i
}