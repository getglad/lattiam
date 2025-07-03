package expressions

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"net"
	"net/url"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// base64EncodeFunc encodes a string to base64
func base64EncodeFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "str",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			return cty.StringVal(base64.StdEncoding.EncodeToString([]byte(args[0].AsString()))), nil
		},
	})
}

// base64DecodeFunc decodes a base64 string
func base64DecodeFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "str",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			s := args[0].AsString()
			sDec, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return cty.UnknownVal(cty.String), fmt.Errorf("failed to decode base64 data: %w", err)
			}
			return cty.StringVal(string(sDec)), nil
		},
	})
}

// urlEncodeFunc URL-encodes a string
func urlEncodeFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "str",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			return cty.StringVal(url.QueryEscape(args[0].AsString())), nil
		},
	})
}

// Hash functions
func md5Func() function.Function {
	return makeStringHashFunction(md5.New, hex.EncodeToString)
}

func sha1Func() function.Function {
	return makeStringHashFunction(sha1.New, hex.EncodeToString)
}

func sha256Func() function.Function {
	return makeStringHashFunction(sha256.New, hex.EncodeToString)
}

func sha512Func() function.Function {
	return makeStringHashFunction(sha512.New, hex.EncodeToString)
}

// makeStringHashFunction creates a hash function
func makeStringHashFunction(hf func() hash.Hash, enc func([]byte) string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "str",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (ret cty.Value, err error) {
			s := args[0].AsString()
			h := hf()
			h.Write([]byte(s))
			rv := enc(h.Sum(nil))
			return cty.StringVal(rv), nil
		},
	})
}

// lookupFunc looks up a value in a map
//
//nolint:gocognit // complex lookup logic
func lookupFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "inputMap",
				Type: cty.DynamicPseudoType,
			},
			{
				Name: "key",
				Type: cty.String,
			},
		},
		VarParam: &function.Parameter{
			Name: "default",
			Type: cty.DynamicPseudoType,
		},
		Type: function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, _ cty.Type) (ret cty.Value, err error) {
			if len(args) < 2 {
				return cty.NilVal, errors.New("lookup requires at least 2 arguments")
			}

			mapVar := args[0]
			key := args[1]

			if mapVar.Type().IsObjectType() {
				mapVar, err = convertObjectToMap(mapVar)
				if err != nil {
					return cty.NilVal, err
				}
			}

			if !mapVar.Type().IsMapType() {
				return cty.NilVal, errors.New("lookup() requires a map as the first argument")
			}

			if !mapVar.IsKnown() {
				return cty.UnknownVal(cty.DynamicPseudoType), nil
			}

			if mapVar.IsNull() {
				if len(args) == 3 {
					return args[2], nil
				}
				return cty.NilVal, errors.New("lookup failed: map is null")
			}

			if !key.IsKnown() {
				return cty.UnknownVal(cty.DynamicPseudoType), nil
			}

			keyStr := key.AsString()
			mapVals := mapVar.AsValueMap()

			if val, ok := mapVals[keyStr]; ok {
				return val, nil
			}

			if len(args) == 3 {
				return args[2], nil
			}

			return cty.NilVal, fmt.Errorf("lookup failed: key %q not found in map", keyStr)
		},
	})
}

// convertObjectToMap converts an object to a map with string keys
func convertObjectToMap(obj cty.Value) (cty.Value, error) {
	if !obj.Type().IsObjectType() {
		return cty.NilVal, errors.New("expected object type")
	}

	vals := make(map[string]cty.Value)
	for k, v := range obj.AsValueMap() {
		vals[k] = v
	}

	if len(vals) == 0 {
		return cty.MapValEmpty(cty.DynamicPseudoType), nil
	}

	return cty.MapVal(vals), nil
}

// transposeFunc transposes a map of lists
//
//nolint:gocognit // complex transpose logic
func transposeFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "values",
				Type: cty.Map(cty.List(cty.String)),
			},
		},
		Type: function.StaticReturnType(cty.Map(cty.List(cty.String))),
		Impl: func(args []cty.Value, _ cty.Type) (ret cty.Value, err error) {
			inputMap := args[0]
			if !inputMap.IsWhollyKnown() {
				return cty.UnknownVal(cty.Map(cty.List(cty.String))), nil
			}

			outputMap := make(map[string][]cty.Value)
			tmpMap := make(map[string]map[string]struct{})

			for k, list := range inputMap.AsValueMap() {
				if !list.IsWhollyKnown() {
					return cty.UnknownVal(cty.Map(cty.List(cty.String))), nil
				}

				for it := list.ElementIterator(); it.Next(); {
					_, v := it.Element()
					if !v.IsWhollyKnown() {
						return cty.UnknownVal(cty.Map(cty.List(cty.String))), nil
					}
					vStr := v.AsString()

					if tmpMap[vStr] == nil {
						tmpMap[vStr] = make(map[string]struct{})
					}
					tmpMap[vStr][k] = struct{}{}
				}
			}

			for v, keys := range tmpMap {
				for k := range keys {
					outputMap[v] = append(outputMap[v], cty.StringVal(k))
				}
			}

			output := make(map[string]cty.Value)
			for k, v := range outputMap {
				output[k] = cty.ListVal(v)
			}

			if len(output) == 0 {
				return cty.MapValEmpty(cty.List(cty.String)), nil
			}

			return cty.MapVal(output), nil
		},
	})
}

// IP network functions
func cidrHostFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "prefix",
				Type: cty.String,
			},
			{
				Name: "hostnum",
				Type: cty.Number,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (ret cty.Value, err error) {
			_, network, err := net.ParseCIDR(args[0].AsString())
			if err != nil {
				return cty.UnknownVal(cty.String), fmt.Errorf("invalid CIDR expression: %w", err)
			}

			hostNum, _ := args[1].AsBigFloat().Int64()

			// Calculate the host IP
			ip := network.IP
			for i := len(ip) - 1; i >= 0 && hostNum > 0; i-- {
				ip[i] += byte(hostNum & 0xff)
				hostNum >>= 8
			}

			if !network.Contains(ip) {
				return cty.UnknownVal(cty.String), errors.New("prefix doesn't have enough host bits to accommodate a host number that large")
			}

			return cty.StringVal(ip.String()), nil
		},
	})
}

func cidrNetmaskFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "prefix",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (ret cty.Value, err error) {
			_, network, err := net.ParseCIDR(args[0].AsString())
			if err != nil {
				return cty.UnknownVal(cty.String), fmt.Errorf("invalid CIDR expression: %w", err)
			}

			return cty.StringVal(net.IP(network.Mask).String()), nil
		},
	})
}

func cidrSubnetFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "prefix",
				Type: cty.String,
			},
			{
				Name: "newbits",
				Type: cty.Number,
			},
			{
				Name: "netnum",
				Type: cty.Number,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (ret cty.Value, err error) {
			_, network, err := net.ParseCIDR(args[0].AsString())
			if err != nil {
				return cty.UnknownVal(cty.String), fmt.Errorf("invalid CIDR expression: %w", err)
			}

			newBits, _ := args[1].AsBigFloat().Int64()
			netNum, _ := args[2].AsBigFloat().Int64()

			// Calculate new prefix length
			ones, bits := network.Mask.Size()
			newOnes := ones + int(newBits)

			if newOnes > bits {
				return cty.UnknownVal(cty.String), fmt.Errorf("not enough address space for %d additional bits", newBits)
			}

			// Calculate the subnet
			newMask := net.CIDRMask(newOnes, bits)
			newNetwork := &net.IPNet{
				IP:   network.IP,
				Mask: newMask,
			}

			// Apply the network number
			subnetBits := uint64(netNum) << uint(bits-newOnes)
			bytes := newNetwork.IP
			for i := len(bytes) - 1; i >= 0 && subnetBits > 0; i-- {
				bytes[i] |= byte(subnetBits & 0xff)
				subnetBits >>= 8
			}

			return cty.StringVal(newNetwork.String()), nil
		},
	})
}
