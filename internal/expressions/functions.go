package expressions

import (
	"time"

	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// GetCoreFunctions returns a minimal set of Terraform-compatible functions
//
//nolint:funlen // Simple map initialization
func GetCoreFunctions() map[string]function.Function {
	return map[string]function.Function{
		// String functions
		"lower":      stdlib.LowerFunc,
		"upper":      stdlib.UpperFunc,
		"trim":       stdlib.TrimFunc,
		"trimspace":  stdlib.TrimSpaceFunc,
		"split":      stdlib.SplitFunc,
		"join":       stdlib.JoinFunc,
		"replace":    stdlib.ReplaceFunc,
		"substr":     stdlib.SubstrFunc,
		"format":     stdlib.FormatFunc,
		"formatlist": stdlib.FormatListFunc,
		"indent":     stdlib.IndentFunc,
		"title":      stdlib.TitleFunc,
		"trimprefix": stdlib.TrimPrefixFunc,
		"trimsuffix": stdlib.TrimSuffixFunc,

		// Numeric functions
		"abs":    stdlib.AbsoluteFunc,
		"ceil":   stdlib.CeilFunc,
		"floor":  stdlib.FloorFunc,
		"max":    stdlib.MaxFunc,
		"min":    stdlib.MinFunc,
		"pow":    stdlib.PowFunc,
		"signum": stdlib.SignumFunc,
		"log":    stdlib.LogFunc,

		// Collection functions
		"length":          stdlib.LengthFunc,
		"concat":          stdlib.ConcatFunc,
		"contains":        stdlib.ContainsFunc,
		"element":         stdlib.ElementFunc,
		"index":           stdlib.IndexFunc,
		"keys":            stdlib.KeysFunc,
		"values":          stdlib.ValuesFunc,
		"lookup":          lookupFunc(),
		"merge":           stdlib.MergeFunc,
		"reverse":         stdlib.ReverseListFunc,
		"sort":            stdlib.SortFunc,
		"compact":         stdlib.CompactFunc,
		"distinct":        stdlib.DistinctFunc,
		"flatten":         stdlib.FlattenFunc,
		"coalescelist":    stdlib.CoalesceListFunc,
		"setintersection": stdlib.SetIntersectionFunc,
		"setproduct":      stdlib.SetProductFunc,
		"setsubtract":     stdlib.SetSubtractFunc,
		"setunion":        stdlib.SetUnionFunc,
		"slice":           stdlib.SliceFunc,
		"transpose":       transposeFunc(),
		"zipmap":          stdlib.ZipmapFunc,
		"range":           stdlib.RangeFunc,
		"chunklist":       stdlib.ChunklistFunc,

		// Type conversion functions
		"tostring": stdlib.MakeToFunc(cty.String),
		"tonumber": stdlib.MakeToFunc(cty.Number),
		"tobool":   stdlib.MakeToFunc(cty.Bool),
		"tolist":   stdlib.MakeToFunc(cty.List(cty.DynamicPseudoType)),
		"tomap":    stdlib.MakeToFunc(cty.Map(cty.DynamicPseudoType)),
		"toset":    stdlib.MakeToFunc(cty.Set(cty.DynamicPseudoType)),

		// Encoding functions
		"jsonencode":   stdlib.JSONEncodeFunc,
		"jsondecode":   stdlib.JSONDecodeFunc,
		"csvdecode":    stdlib.CSVDecodeFunc,
		"base64encode": base64EncodeFunc(),
		"base64decode": base64DecodeFunc(),
		"urlencode":    urlEncodeFunc(),

		// Date/time functions
		"timestamp":  timestampFunc(),
		"formatdate": stdlib.FormatDateFunc,
		"timeadd":    stdlib.TimeAddFunc,

		// Crypto functions
		"uuid":   uuidFunc(),
		"md5":    md5Func(),
		"sha1":   sha1Func(),
		"sha256": sha256Func(),
		"sha512": sha512Func(),

		// Regex functions
		"regex":    stdlib.RegexFunc,
		"regexall": stdlib.RegexAllFunc,

		// Logic functions
		"can": tryfunc.CanFunc,
		"try": tryfunc.TryFunc,

		// Filesystem functions (disabled for security in API context)
		// "file":      disabled
		// "fileexists": disabled
		// "fileset":   disabled
		// "filebase64": disabled

		// IP network functions
		"cidrhost":    cidrHostFunc(),
		"cidrnetmask": cidrNetmaskFunc(),
		"cidrsubnet":  cidrSubnetFunc(),
	}
}

// timestampFunc returns current timestamp in RFC3339 format
func timestampFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(_ []cty.Value, _ cty.Type) (cty.Value, error) {
			return cty.StringVal(time.Now().UTC().Format(time.RFC3339)), nil
		},
	})
}

// uuidFunc generates a random UUID
func uuidFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(_ []cty.Value, _ cty.Type) (cty.Value, error) {
			result, err := uuid.GenerateUUID()
			if err != nil {
				return cty.UnknownVal(cty.String), err
			}
			return cty.StringVal(result), nil
		},
	})
}
