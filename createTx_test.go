package qtumtxsigner

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/qtumproject/qtumsuite"
)

func TestCreateTx(t *testing.T) {
	//qtumsuite should be able to use precise decimals instead of int64
	//Params are (PrivKey, ToAddress, Amount)
	rawTx, err := Tx("cMbgxCJrTYUqgcmiC1berh5DFrtY1KeU4PXZ6NZxgenniF1mXCRk", "qLn9vqbr2Gx3TsVR9QyTVB5mrMoh4x43Uf", 200000000)
	if err != nil {
		fmt.Println("Err coming from CreateTx")
		fmt.Println(err)
	}

	// Create Tx result:
	//01000000010000000000000000000000000000000000000000000000000000000000000000000000008a47304402201dea8d03377e9e594ad3d647f1da156f383c1c0670b56ef0134d17b17311d019022002d87e4f03d3431efdf91468127dc7fb8995495cafe97a6afb7f84ca136ed13601410499d391f528b9edd07284c7e23df8415232a8ce41531cf460a390ce32b4efd112001102ddf975544f913aca6119377a479a51cd3587b1aa383adb5794c844f776ffffffff0164000000000000001976a9142352be3db3177f0a07efbe6da5857615b8c9901d88ac00000000
	//Compiled binary found in janus Readme:
	//608060405234801561001057600080fd5b506040516020806100f2833981016040525160005560bf806100336000396000f30060806040526004361060485763ffffffff7c010000000000000000000000000000000000000000000000000000000060003504166360fe47b18114604d5780636d4ce63c146064575b600080fd5b348015605857600080fd5b5060626004356088565b005b348015606f57600080fd5b506076608d565b60408051918252519081900360200190f35b600055565b600054905600a165627a7a7230582049a087087e1fc6da0b68ca259d45a2e369efcbb50e93f9b7fa3e198de6402b8100290000000000000000000000000000000000000000000000000000000000000001
	fmt.Println("raw signed transaction is: ", rawTx)
}

func TestCallContractTx(t *testing.T) {

	var abiJson = `[{"inputs":[],"name":"get","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"x","type":"uint256"}],"name":"set","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
	arguments := map[string][]interface{}{
		"get": {},
	}
	callData, err := CallContractData(strings.NewReader(abiJson), arguments)
	rawTx, err := CallTx("cMbgxCJrTYUqgcmiC1berh5DFrtY1KeU4PXZ6NZxgenniF1mXCRk", "qUbxboqjBRp96j3La8D1RYkyqx5uQbJPoW", "dcb58d4670a6922abc89d5fc1aea38316ee7e373", 2000000000, callData, 2500000, 40)
	if err != nil {
		fmt.Println("Err coming from CreateContractTx")
		fmt.Println(err)
	}

	fmt.Println("raw signed transaction is: ", rawTx)
}

func TestABI(t *testing.T) {
	const definition = `[{"inputs":[],"name":"get","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"x","type":"uint256"}],"name":"set","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

	abi, err := abi.JSON(strings.NewReader(definition))
	if err != nil {
		panic(err)
	}
	out, err := abi.Pack("set", big.NewInt(5))
	if err != nil {
		panic(err)
	}
	fmt.Println("ByteCode: ", hex.EncodeToString(out))
}

func TestContractData(t *testing.T) {
	//Reading the ABI
	var abiJson = `[{"inputs":[],"name":"get","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"x","type":"uint256"}],"name":"set","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
	parsedABI, err := abi.JSON(strings.NewReader(abiJson))
	if err != nil {
		fmt.Println("Error reading abi JSON: ", err)
	}
	//Packing the ABI
	var bytecode []byte
	for _, m := range parsedABI.Methods {
		var methodTypes []interface{}
		fmt.Println("Method name: ", m.Name)
		for _, i := range m.Inputs {
			methodTypes = append(methodTypes, i.Type.GetType())

		}
		fmt.Println("Method input types: ", methodTypes)
		bytecode, err = parsedABI.Pack(m.Name, methodTypes...)
		if err != nil {
			fmt.Println("Could not pack:", err)
		}

	}
	fmt.Printf("%x\n: ", hex.EncodeToString(bytecode))
}

func TestRPCRequest(t *testing.T) {

	wif, err := qtumsuite.DecodeWIF("cMbgxCJrTYUqgcmiC1berh5DFrtY1KeU4PXZ6NZxgenniF1mXCRk")
	if err != nil {
		fmt.Println(err)
	}

	keyid := qtumsuite.Hash160(wif.SerializePubKey())
	params := []interface{}{"0x" + hex.EncodeToString(keyid), 0.005}
	data := JSONRPCRequest{
		ID:      "10",
		Jsonrpc: "2.0",
		Method:  "qtum_getUTXOs",
		Params:  params,
	}

	payloadBytes, err := json.Marshal(data)
	if err != nil {
		fmt.Println(err)
	}
	body := bytes.NewReader(payloadBytes)
	//Link to RPC
	req, err := http.NewRequest("POST", "http://localhost:23889", body)
	if err != nil {
		fmt.Println(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
	}
	defer resp.Body.Close()

	var cResp JSONRPCResult

	if err := json.NewDecoder(resp.Body).Decode(&cResp); err != nil {
		fmt.Println("ooopsss! an error occurred, please try again")
	}

	var listUnspentResp *ListUnspentResponse

	if err := json.Unmarshal(cResp.RawResult, &listUnspentResp); err != nil {
		fmt.Println(err)
	}

	fmt.Println(listUnspentResp)

}
