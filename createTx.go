package qtumtxsigner

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/qtumproject/qtumsuite"
	"github.com/qtumproject/qtumsuite/chaincfg"
	"github.com/qtumproject/qtumsuite/chaincfg/chainhash"
	"github.com/qtumproject/qtumsuite/txscript"
	"github.com/qtumproject/qtumsuite/wire"
	"github.com/shopspring/decimal"
)

type JSONRPCRequest struct {
	ID      string        `json:"id"`
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type JSONRPCResult struct {
	JSONRPC   string          `json:"jsonrpc"`
	RawResult json.RawMessage `json:"result,omitempty"`
	Error     *JSONRPCError   `json:"error,omitempty"`
	ID        json.RawMessage `json:"id"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ListUnspentResponse []struct {
	Address       string          `json:"address"`
	Txid          string          `json:"txid"`
	Vout          uint            `json:"vout"`
	Amount        decimal.Decimal `json:"amount"`
	Safe          bool            `json:"safe"`
	Spendable     bool            `json:"spendable"`
	Solvable      bool            `json:"solvable"`
	Label         string          `json:"label"`
	Confirmations int             `json:"confirmations"`
	ScriptPubKey  string          `json:"scriptPubKey"`
	RedeemScript  string          `json:"redeemScript"`
}

var qtumTestNetParams = chaincfg.MainNetParams
var (
	OP_CREATE = byte(0xc1)
	OP_CALL   = byte(0xc2)
)

func init() {

	//TestnetParams
	qtumTestNetParams.PubKeyHashAddrID = 120
	qtumTestNetParams.ScriptHashAddrID = 110

}

//Create something like a map where "name"->inputs or Look at ABI gen
func CallContractData(reader io.Reader, arguments map[string][]interface{}) ([]byte, error) {
	parsedABI, err := abi.JSON(reader)
	if err != nil {
		fmt.Println("Error reading abi JSON: ")
		return []byte{}, err
	}
	//Packing the ABI
	var bytecode []byte
	for name, inputs := range arguments {
		var methodInputs []interface{}
		//Loop through arguments and create the list of inputs
		fmt.Println("Method name: ", name)
		for _, input := range inputs {
			methodInputs = append(methodInputs, input)
		}
		//Pack the inputs into the method
		fmt.Println("Method Inputs: ", methodInputs)
		bytecode, err = parsedABI.Pack(name, methodInputs...)
		if err != nil {
			fmt.Println("Could not pack input: ", err)
			return []byte{}, err
		}

	}
	return bytecode, nil
}

func GatherUTXOs(serilizedPubKey []byte, sourceTx *wire.MsgTx) (*ListUnspentResponse, int64, error) {

	//Get UTXOs from network
	//Use the UTXOs to figure out the previousTxId as well as the pubKeyScript
	/* LOOK INTO JANUS TAKING ADDRESSES WITHOUT THE 0x PREFIX AND STILL RETURNING A BALANCE*/
	keyid := qtumsuite.Hash160(serilizedPubKey)
	params := []interface{}{"0x" + hex.EncodeToString(keyid), 0.005}
	data := JSONRPCRequest{
		ID:      "10",
		Jsonrpc: "2.0",
		Method:  "qtum_getUTXOs",
		Params:  params,
	}

	payloadBytes, err := json.Marshal(data)
	if err != nil {
		return nil, 0, err
	}
	body := bytes.NewReader(payloadBytes)
	//Link to RPC
	req, err := http.NewRequest("POST", "http://localhost:23889", body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var cResp JSONRPCResult

	if err := json.NewDecoder(resp.Body).Decode(&cResp); err != nil {
		return nil, 0, err
	}

	var listUnspentResp *ListUnspentResponse

	if err := json.Unmarshal(cResp.RawResult, &listUnspentResp); err != nil {
		return nil, 0, err
	}

	balance := decimal.NewFromFloat(0)
	for _, utxo := range *listUnspentResp {
		balance = balance.Add(utxo.Amount)
	}

	balance = balance.Mul(decimal.NewFromFloat(1e8))
	floatBalance, exact := balance.Float64()

	if exact != true {
		return nil, 0, err
	}

	return listUnspentResp, int64(floatBalance), nil
}

func P2khTx(privKey string, destination string, amount int64) (string, error) {

	redeemTx := wire.NewMsgTx(wire.TxVersion)

	//Decode WIF
	wif, err := qtumsuite.DecodeWIF(privKey)
	if err != nil {
		return "", err
	}

	//Gather info extracted from UTXOs related to addrPubKey (prevTxId, balance, pkScript)
	utxos, balance, err := GatherUTXOs(wif.SerializePubKey(), redeemTx)
	if err != nil {
		return "", err
	}

	//Checking for sufficient balance
	if balance < amount {
		return "", fmt.Errorf("insufficient balance")
	}

	//Loop through UTXO to find candidates
	var amountIn int64 = 0
	var pkScripts [][]byte
	for _, v := range *utxos {

		utxoHash, err := chainhash.NewHashFromStr(v.Txid)
		if err != nil {
			fmt.Println("could not get hash from transaction ID; error:", err)
			return "", err
		}

		outPoint := wire.NewOutPoint(utxoHash, uint32(v.Vout))
		txIn := wire.NewTxIn(outPoint, nil, nil)

		floatAmount := v.Amount.Mul(decimal.NewFromFloat(1e8))
		utxoAmount, exact := floatAmount.Float64()
		if exact != true {
			fmt.Println("could not convert utxoAmount from decimal to float precisely; err:", err)
			return "", err
		}

		amountIn += int64(utxoAmount)

		//Append ScriptPubKey to the list of scripts
		utxoPkScript, err := hex.DecodeString(v.ScriptPubKey)
		if err != nil {
			return "", err
		}
		pkScripts = append(pkScripts, utxoPkScript)

		//Append Transaction
		redeemTx.AddTxIn(txIn)

		//Once we gathered all the UTXOs we need, we stop
		if amountIn >= amount {
			break
		}

	}

	//Get destination address as []byte from function argument (destination)
	destinationAddr, err := qtumsuite.DecodeAddress(destination, &qtumTestNetParams)
	if err != nil {
		return "", err
	}

	//Generate PayToAddressScript
	destinationScript, err := txscript.PayToAddrScript(destinationAddr)
	if err != nil {
		return "", err
	}

	/*
		ADD OP CODES FOR CONTRACT CREATION TO THE TX OUTPUT

	*/

	//Adding the destination address and the amount to the transaction as output
	redeemTxOut := wire.NewTxOut(amount, destinationScript)
	redeemTx.AddTxOut(redeemTxOut)

	//Might want to look into a non hard coded way to calculate this
	var change int64 = amountIn - amount - 100000

	//Get address
	addrPubKey, err := qtumsuite.NewAddressPubKey(wif.SerializePubKey(), &chaincfg.TestNet3Params)

	//Generate PayToAddrScript for source address
	changeScript, err := txscript.PayToAddrScript(addrPubKey)
	if err != nil {
		return "", err
	}

	chanceTxOut := wire.NewTxOut(change, changeScript)
	redeemTx.AddTxOut(chanceTxOut)

	// Sign the Tx
	finalRawTx, err := SignTx(redeemTx, pkScripts, wif)

	return finalRawTx, nil
}

func ContractTx(privKey string, from string, contractAddr string, amount int64, data []byte, gas int64, gasPrice int64, opcode byte) (string, error) {

	redeemTx := wire.NewMsgTx(wire.TxVersion)

	//Decode WIF
	wif, err := qtumsuite.DecodeWIF(privKey)
	if err != nil {
		return "", err
	}

	//Gather info extracted from UTXOs related to addrPubKey (prevTxId, balance, pkScript)
	utxos, balance, err := GatherUTXOs(wif.SerializePubKey(), redeemTx)
	if err != nil {
		return "", err
	}

	//Checking for sufficient balance
	if balance < amount {
		return "", fmt.Errorf("insufficient balance")
	}

	//Loop through UTXO to find candidates
	var amountIn int64 = 0
	var pkScripts [][]byte
	for _, v := range *utxos {

		utxoHash, err := chainhash.NewHashFromStr(v.Txid)
		if err != nil {
			fmt.Println("could not get hash from transaction ID; error:", err)
			return "", err
		}

		outPoint := wire.NewOutPoint(utxoHash, uint32(v.Vout))
		txIn := wire.NewTxIn(outPoint, nil, nil)

		floatAmount := v.Amount.Mul(decimal.NewFromFloat(1e8))
		utxoAmount, exact := floatAmount.Float64()
		if exact != true {
			fmt.Println("could not convert utxoAmount from decimal to float precisely; err:", err)
			return "", err
		}

		amountIn += int64(utxoAmount)

		//Append ScriptPubKey to the list of scripts
		utxoPkScript, err := hex.DecodeString(v.ScriptPubKey)
		if err != nil {
			return "", err
		}
		pkScripts = append(pkScripts, utxoPkScript)

		//Append Transaction
		redeemTx.AddTxIn(txIn)

		//Once we gathered all the UTXOs we need, we stop
		if amountIn >= amount {
			break
		}

	}

	var change int64 = amountIn - amount - gas*gasPrice

	//Get address
	addrPubKey, err := qtumsuite.NewAddressPubKey(wif.SerializePubKey(), &chaincfg.TestNet3Params)

	//Generate PayToAddrScript for source address
	changeScript, err := txscript.PayToAddrScript(addrPubKey)
	if err != nil {
		return "", err
	}

	changeTxOut := wire.NewTxOut(change, changeScript)

	fromAddr, err := qtumsuite.DecodeAddress(from, &qtumTestNetParams)
	if err != nil {
		return "", err
	}

	//Generate PayToAddressScript
	senderScript, err := txscript.PayToAddrScript(fromAddr)
	if err != nil {
		return "", err
	}

	senderTxOut := wire.NewTxOut(amount, senderScript)
	redeemTx.AddTxOut(senderTxOut)

	contractScript, err := ContractScript(redeemTx, wif, data, contractAddr, opcode)
	if err != nil {
		fmt.Println("Something went wrong with the contract script: ", err)
		return "", err
	}

	//Build vouts

	//Adding the destination address and the amount to the transaction as output
	redeemTxOut := wire.NewTxOut(0, contractScript)
	redeemTx.AddTxOut(redeemTxOut)

	//Add change to tx out
	redeemTx.AddTxOut(changeTxOut)

	// Sign the Tx
	finalRawTx, err := SignTx(redeemTx, pkScripts, wif)

	return finalRawTx, nil
}

//Creates pubKeyScript of to create or call a contract with data depending on the byte used for opcode
// a 0xc2 byte (OP_CALL) will call a contract with the data, while a 0xc1 byte (OP_CREATE) will create
// a contract with the data
func ContractScript(redeemTx *wire.MsgTx, wif *qtumsuite.WIF, data []byte, contractAddr string, opcode byte) ([]byte, error) {

	//Build scriptPubKey
	scriptBuilder := txscript.NewScriptBuilder()
	scriptBuilder.AddData([]byte{4})  //EVM Version
	scriptBuilder.AddInt64(2500000)   //gas limit
	scriptBuilder.AddData([]byte{40}) //Gas price
	scriptBuilder.AddData(data)       //contract data
	if opcode == OP_CALL {
		hexContractAddr, err := hex.DecodeString(contractAddr)
		if err != nil {
			fmt.Println("odd length coming from data")
			return []byte{0}, err
		}
		scriptBuilder.AddData(hexContractAddr)
	}
	//contract address
	scriptBuilder.AddOp(opcode) // Add OP_CODE byte (0xc2 -> OP_CALL, 0xc1 -> OP_CREATE)

	createScript, err := scriptBuilder.Script()
	if err != nil {
		return []byte{0}, err
	}
	return createScript, nil
}

func SignTx(redeemTx *wire.MsgTx, sourcePkScript [][]byte, wif *qtumsuite.WIF) (string, error) {

	for i := range redeemTx.TxIn {

		//Generate signature script

		signatureHash, err := txscript.CalcSignatureHash(sourcePkScript[i], txscript.SigHashAll, redeemTx, i)
		if err != nil {
			return "", err
		}

		/*
			Sometimes the signing process doesn't work
		*/
		privKey := secp256k1.PrivKeyFromBytes(wif.PrivKey.Serialize())

		signature := ecdsa.Sign(privKey, signatureHash)
		//Adding .AddData(wif.SerializePubKey()) causes issues with P2PKH transactions
		signatureScript, err := txscript.NewScriptBuilder().AddData(append(signature.Serialize(), byte(txscript.SigHashAll))).Script()
		if err != nil {
			return "", err
		}

		redeemTx.TxIn[i].SignatureScript = signatureScript
	}

	buf := bytes.NewBuffer(make([]byte, 0, redeemTx.SerializeSize()))
	redeemTx.Serialize(buf)

	hexSignedTx := hex.EncodeToString(buf.Bytes())

	return hexSignedTx, nil
}
