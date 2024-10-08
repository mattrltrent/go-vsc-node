package dids_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"vsc-node/lib/dids"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	blocks "github.com/ipfs/go-block-format"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
)

func TestEthDIDVerify(t *testing.T) {
	// data to be signed and verified
	data := map[string]any{
		// we have 2 fields of data to ensure deterministic encoding and decoding
		//
		// this is because if our encoding and decoding is not deterministic, the signature
		// will differ each time we sign this data because the order will switch since Go maps
		// don't guarantee order
		"foo": "bar",
		"baz": 12345,
	}

	// encode data into CBOR and create a block
	cborData, err := cbor.WrapObject(data, multihash.SHA2_256, -1)
	assert.Nil(t, err)

	// create a block with the CBOR data
	block, err := blocks.NewBlockWithCid(cborData.RawData(), cborData.Cid())
	assert.Nil(t, err)

	// gen a priv key for signing
	privateKey, err := crypto.GenerateKey()
	assert.Nil(t, err)

	// create a dummy temporary function to sign the data
	sign := func(data map[string]any) (string, error) {
		// convert the data to EIP-712 typed data
		_, err := dids.ConvertToEIP712TypedData("vsc.network", data, "tx_container_v0", func(f float64) (*big.Int, error) {
			// standard (default) conversion of float to big int
			return big.NewInt(int64(f)), nil
		})
		assert.Nil(t, err)

		// compute the EIP-712 hash
		//
		// normally would be `dataHash, err := dids.ComputeEIP712Hash(payload.Data)` but
		// we want to keep this method private, so we'll just hardcode the hash here for this
		// particular unit test case
		dataHash := []byte{15, 233, 134, 98, 193, 209, 180, 13, 124, 237, 174, 183, 79, 181, 206, 254, 125, 138, 91, 249, 230, 243, 91, 195, 137, 142, 164, 209, 201, 90, 216, 177}

		// sign the data hash using the priv key
		bytesOfSig, err := crypto.Sign(dataHash, privateKey)
		assert.Nil(t, err)

		return hex.EncodeToString(bytesOfSig), nil
	}

	// use the dummy function we created to sign the data
	signature, err := sign(data)
	assert.Nil(t, err)

	// verify the sig using the EthDID
	ethDID := dids.NewEthDID(crypto.PubkeyToAddress(privateKey.PublicKey).Hex())
	isValid, err := ethDID.Verify(block, signature)
	assert.Nil(t, err)
	assert.True(t, isValid)
}

func TestNewEthDID(t *testing.T) {
	ethAddr := "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
	did := dids.NewEthDID(ethAddr)

	expectedDID := dids.EthDIDPrefix + ethAddr
	assert.Equal(t, expectedDID, did.String())
}

func TestConvertToEIP712TypedDataInvalidDomain(t *testing.T) {
	data := map[string]interface{}{"name": "Alice"}

	_, err := dids.ConvertToEIP712TypedData("", data, "tx_container_v0", func(f float64) (*big.Int, error) {
		return big.NewInt(int64(f)), nil
	})
	assert.NotNil(t, err)
}

func TestConvertToEIP712TypedDataInvalidPrimaryTypename(t *testing.T) {
	data := map[string]interface{}{"name": "Alice"}

	_, err := dids.ConvertToEIP712TypedData("vsc.network", data, "", func(f float64) (*big.Int, error) {
		return big.NewInt(int64(f)), nil
	})
	assert.NotNil(t, err)
}

func TestEIP712InvalidTypes(t *testing.T) {
	data := map[string]interface{}{
		"myFunc": func() {},
		"myChan": make(chan int),
	}

	_, err := dids.ConvertToEIP712TypedData("vsc.network", data, "tx_container_v0", func(f float64) (*big.Int, error) {
		return big.NewInt(int64(f)), nil
	})

	// invalid types SHOULD throw errors
	assert.NotNil(t, err)
}

func TestEIP712ComplexSliceArrayData(t *testing.T) {
	// we need to be able to confirm these types in the EIP-712 typed data, since they are difficult edge cases
	data := map[string]interface{}{
		"names":        []interface{}{"Alice", "Bob"},
		"ages":         []interface{}{25, 30},
		"someByteData": []byte{0x01, 0x02, 0x03},
		"marks":        []interface{}{25.5, 30.5},
	}

	// convert data into EIP-712 typed data
	typedData, err := dids.ConvertToEIP712TypedData("vsc.network", data, "tx_container_v0", func(f float64) (*big.Int, error) {
		return big.NewInt(int64(f)), nil
	})
	assert.Nil(t, err)

	// marshal the output for manual field assertions
	marshalled, err := typedData.MarshalJSON()
	assert.Nil(t, err)

	// unmarshal the output for easier assertions
	var result map[string]interface{}
	err = json.Unmarshal(marshalled, &result)
	assert.Nil(t, err)

	// assert that the types field exists
	typesField, ok := result["types"].(map[string]interface{})
	assert.True(t, ok)

	// ensure tx_container_v0 field exists in types
	txContainerField, ok := typesField["tx_container_v0"].([]interface{})
	assert.True(t, ok)

	// helper func to find a type by name
	findFieldsType := func(fields []interface{}, name string) string {
		for _, field := range fields {
			fieldMap, ok := field.(map[string]interface{})
			if ok && fieldMap["name"] == name {
				return fieldMap["type"].(string)
			}
		}
		return ""
	}

	// assert the types of the fields match the expected types
	assert.Equal(t, "string[]", findFieldsType(txContainerField, "names"))
	assert.Equal(t, "int256[]", findFieldsType(txContainerField, "ages"))
	assert.Equal(t, "uint256[]", findFieldsType(txContainerField, "marks"))
	assert.Equal(t, "bytes", findFieldsType(txContainerField, "someByteData"))

	// assert the primary type matches what we expect
	assert.Equal(t, "tx_container_v0", result["primaryType"])

	// assert domain matches
	domainField, ok := result["domain"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, domainField["name"], "vsc.network")
}

func TestCreateEthDIDProvider(t *testing.T) {
	provider := dids.NewEthProvider()
	assert.NotNil(t, provider)
}

// exact real data case from the Bitcoin wrapper UI
func TestEIP712RealDataCase(t *testing.T) {
	// structure to match the required JSON schema for a tx on the Bitoin wrapper UI: https://github.com/vsc-eco/Bitcoin-wrap-UI
	// with some potentially sensitive data replaced with XXXXXs and YYYYYs
	data := map[string]interface{}{
		"tx": map[string]interface{}{
			"op": "transfer",
			"payload": map[string]interface{}{
				"tk":     "HIVE",
				"to":     "hive:XXXXX",
				"from":   "did:pkh:eip155:1:YYYYY",
				"amount": uint64(1),
			},
		},
		"__t": "vsc-tx",
		"__v": "0.2",
		"headers": map[string]interface{}{
			"type":    uint64(1),
			"nonce":   uint64(1),
			"intents": []interface{}{},
			"required_auths": []string{
				"did:pkh:eip155:1:YYYYY",
			},
		},
	}

	// convert data into EIP-712 typed data
	typedDataFromOurConversion, err := dids.ConvertToEIP712TypedData("vsc.network", data, "tx_container_v0", func(f float64) (*big.Int, error) {
		return big.NewInt(int64(f)), nil
	})
	assert.Nil(t, err)

	// a real inspected transaction from the Bitcoin wrapper UI (https://github.com/vsc-eco/Bitcoin-wrap-UI)
	// that we try to "match" exactly via the EIP-712 typed data input that we create above (data var)
	//
	// except the username of the tx and the DIDs are different for privacy reasons (XXXXXs and YYYYYs)
	realSystemTypedData := `
{
    "EIP712Domain": [
        {
            "name": "name",
            "type": "string"
        }
    ],
    "types": {
        "tx_container_v0.tx.payload": [
            {
                "name": "tk",
                "type": "string"
            },
            {
                "name": "to",
                "type": "string"
            },
            {
                "name": "from",
                "type": "string"
            },
            {
                "name": "amount",
                "type": "uint256"
            }
        ],
        "tx_container_v0.tx": [
            {
                "name": "op",
                "type": "string"
            },
            {
                "name": "payload",
                "type": "tx_container_v0.tx.payload"
            }
        ],
        "tx_container_v0.headers": [
            {
                "name": "type",
                "type": "uint256"
            },
            {
                "name": "nonce",
                "type": "uint256"
            },
            {
                "name": "intents",
                "type": "undefined[]"
            },
            {
                "name": "required_auths",
                "type": "string[]"
            }
        ],
        "tx_container_v0": [
            {
                "name": "tx",
                "type": "tx_container_v0.tx"
            },
            {
                "name": "__t",
                "type": "string"
            },
            {
                "name": "__v",
                "type": "string"
            },
            {
                "name": "headers",
                "type": "tx_container_v0.headers"
            }
        ]
    },
    "primaryType": "tx_container_v0",
    "message": {
        "tx": {
            "op": "transfer",
            "payload": {
                "tk": "HIVE",
                "to": "hive:XXXXX",
                "from": "did:pkh:eip155:1:YYYYY",
                "amount": 1
            }
        },
        "__t": "vsc-tx",
        "__v": "0.2",
        "headers": {
            "type": 1,
            "nonce": 1,
            "intents": [],
            "required_auths": [
                "did:pkh:eip155:1:YYYYY"
            ]
        }
    },
    "domain": {
        "name": "vsc.network"
    }
}
	`

	var realSystemTypedDataMap map[string]interface{}
	err = json.Unmarshal([]byte(realSystemTypedData), &realSystemTypedDataMap)
	assert.Nil(t, err)

	// marshal and unmarshal our typed data conversion for sake of comparision
	marshalled, err := typedDataFromOurConversion.MarshalJSON()
	assert.Nil(t, err)

	var typedDataFromOurConversionMap map[string]interface{}
	err = json.Unmarshal(marshalled, &typedDataFromOurConversionMap)
	assert.Nil(t, err)

	// custom comparison for map to define equality without caring about order
	opts := cmp.Options{
		cmpopts.SortSlices(func(x, y interface{}) bool {
			xMap, xOk := x.(map[string]interface{})
			yMap, yOk := y.(map[string]interface{})
			if xOk && yOk {
				if xVal, xExists := xMap["name"]; xExists {
					if yVal, yExists := yMap["name"]; yExists {
						return fmt.Sprintf("%v", xVal) < fmt.Sprintf("%v", yVal)
					}
				}
			}
			// if not not a map, fallback to basic comparison of strings
			return fmt.Sprintf("%v", x) < fmt.Sprintf("%v", y)
		}),
		cmpopts.EquateEmpty(), // we consider empty slices equal to nil slices for this
	}

	// if there's no diff, the test passes
	assert.Equal(t, "", cmp.Diff(realSystemTypedDataMap, typedDataFromOurConversionMap, opts...))
}

func TestEIP712FloatHandlerError(t *testing.T) {
	// random valid data
	data := map[string]interface{}{
		"age": 1.5, // float data which will cause the handler error
	}

	// we expect an error because we have a float in our data and we specify in our
	// handler that we don't want floats
	_, err := dids.ConvertToEIP712TypedData("vsc.network", data, "tx_container_v0", func(f float64) (*big.Int, error) {
		return nil, fmt.Errorf("we decide to throw this error if we accidently put a float in our data")
	})
	assert.NotNil(t, err)
}

func TestEIP712EmptyData(t *testing.T) {
	data := map[string]interface{}{}

	// convert data into EIP-712 typed data
	typedData, err := dids.ConvertToEIP712TypedData("vsc.network", data, "tx_container_v0", func(f float64) (*big.Int, error) {
		return big.NewInt(int64(f)), nil
	})
	assert.Nil(t, err)

	// marshal the output for manual field assertions
	marshalled, err := typedData.MarshalJSON()
	assert.Nil(t, err)

	// unmarshal the JSON into a map for individual field checks
	var result map[string]interface{}
	err = json.Unmarshal(marshalled, &result)
	assert.Nil(t, err)

	// check types
	typesField, ok := result["types"].(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, typesField, "tx_container_v0")
	assert.Empty(t, typesField["tx_container_v0"])

	// check primaryType
	assert.Equal(t, result["primaryType"], "tx_container_v0")

	// check domain
	domainField, ok := result["domain"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, domainField["name"], "vsc.network")

	// check message
	messageField, ok := result["message"].(map[string]interface{})
	assert.True(t, ok)
	assert.Empty(t, messageField)

	// check EIP712Domain
	eip712Domain, ok := result["EIP712Domain"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, eip712Domain, 1)

	domainFieldEntry, ok := eip712Domain[0].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, domainFieldEntry["name"], "name")
	assert.Equal(t, domainFieldEntry["type"], "string")
}

func TestEIP712StructData(t *testing.T) {
	dummyStruct := struct {
		Name string
	}{
		Name: "alice",
	}

	// convert data into EIP-712 typed data
	typedData, err := dids.ConvertToEIP712TypedData("vsc.network", dummyStruct, "tx_container_v0", func(f float64) (*big.Int, error) {
		return big.NewInt(int64(f)), nil
	})
	assert.Nil(t, err)

	// assert that alice exists in the msg field
	marshalled, err := typedData.MarshalJSON()
	assert.Nil(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(marshalled, &result)
	assert.Nil(t, err)

	// confirm message field has the correct name and value
	messageField, ok := result["message"].(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, messageField, "Name")
	assert.Equal(t, messageField["Name"], "alice")
}

func TestEIP712ConvertStringToAddr(t *testing.T) {
	// data that is of type string, that should auto-coerce to an address to match EIP-712 spec
	data := map[string]interface{}{
		"wallet": "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
	}

	typedData, err := dids.ConvertToEIP712TypedData("vsc.network", data, "tx_container_v0", func(f float64) (*big.Int, error) {
		return big.NewInt(int64(f)), nil
	})
	assert.Nil(t, err)

	// marshal the output for manual field assertions
	marshalled, err := typedData.MarshalJSON()
	assert.Nil(t, err)

	// unmarshal the JSON into a map for individual field checks
	var result map[string]interface{}
	err = json.Unmarshal(marshalled, &result)
	assert.Nil(t, err)

	// ensure types exists and is in the correct form
	typesField, ok := result["types"].(map[string]interface{})
	assert.True(t, ok)

	// ensure the tx_container_v0 field is present and is a slice
	txContainerField, ok := typesField["tx_container_v0"].([]interface{})
	assert.True(t, ok)

	// loop through the fields in the tx_container_v0 to find wallet and check its type
	var walletField map[string]interface{}
	for _, field := range txContainerField {
		fieldMap, ok := field.(map[string]interface{})
		if ok && fieldMap["name"] == "wallet" {
			walletField = fieldMap
			break
		}
	}

	// ensure wallet is found and is of type address instead of the initial string
	assert.NotNil(t, walletField)
	assert.Contains(t, walletField, "type")
	assert.Equal(t, walletField["type"], "address")
}
