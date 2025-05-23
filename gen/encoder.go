package gen

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/19910211/easyjson"
)

func (g *Generator) getEncoderName(t reflect.Type) string {
	return g.functionName("encode", t)
}

var primitiveEncoders = map[reflect.Kind]string{
	reflect.String:  "out.String(string(%v))",
	reflect.Bool:    "out.Bool(bool(%v))",
	reflect.Int:     "out.Int(int(%v))",
	reflect.Int8:    "out.Int8(int8(%v))",
	reflect.Int16:   "out.Int16(int16(%v))",
	reflect.Int32:   "out.Int32(int32(%v))",
	reflect.Int64:   "out.Int64(int64(%v))",
	reflect.Uint:    "out.Uint(uint(%v))",
	reflect.Uint8:   "out.Uint8(uint8(%v))",
	reflect.Uint16:  "out.Uint16(uint16(%v))",
	reflect.Uint32:  "out.Uint32(uint32(%v))",
	reflect.Uint64:  "out.Uint64(uint64(%v))",
	reflect.Float32: "out.Float32(float32(%v))",
	reflect.Float64: "out.Float64(float64(%v))",
}

var primitiveStringEncoders = map[reflect.Kind]string{
	reflect.String:  "out.String(string(%v))",
	reflect.Int:     "out.IntStr(int(%v))",
	reflect.Int8:    "out.Int8Str(int8(%v))",
	reflect.Int16:   "out.Int16Str(int16(%v))",
	reflect.Int32:   "out.Int32Str(int32(%v))",
	reflect.Int64:   "out.Int64Str(int64(%v))",
	reflect.Uint:    "out.UintStr(uint(%v))",
	reflect.Uint8:   "out.Uint8Str(uint8(%v))",
	reflect.Uint16:  "out.Uint16Str(uint16(%v))",
	reflect.Uint32:  "out.Uint32Str(uint32(%v))",
	reflect.Uint64:  "out.Uint64Str(uint64(%v))",
	reflect.Uintptr: "out.UintptrStr(uintptr(%v))",
	reflect.Float32: "out.Float32Str(float32(%v))",
	reflect.Float64: "out.Float64Str(float64(%v))",
}

// fieldTags contains parsed version of json struct field tags.
type fieldTags struct {
	name string

	omit              bool
	omitEmpty         bool
	noOmitEmpty       bool
	asString          bool
	required          bool
	intern            bool
	noCopy            bool
	sliceValOmitempty bool // 排除掉切片里的 字符串和指针空值
	newStruct         bool // 自定义new Struct
	NewStruct         bool // 自定义New Struct
}

// parseFieldTags parses the json field tag into a structure.
func parseFieldTags(f reflect.StructField) fieldTags {
	var ret fieldTags

	for i, s := range strings.Split(f.Tag.Get("json"), ",") {
		switch {
		case i == 0 && s == "-":
			ret.omit = true
		case i == 0:
			ret.name = s
		case s == "omitempty":
			ret.omitEmpty = true
		case s == "!omitempty":
			ret.noOmitEmpty = true
		case s == "string":
			ret.asString = true
		case s == "required":
			ret.required = true
		case s == "intern":
			ret.intern = true
		case s == "nocopy":
			ret.noCopy = true
		case s == "sliceValOmitempty":
			ret.sliceValOmitempty = true
		case s == "newStruct":
			ret.newStruct = true
		case s == "NewStruct":
			ret.NewStruct = true
		}

	}

	return ret
}

// genTypeEncoder generates code that encodes in of type t into the writer, but uses marshaler interface if implemented by t.
func (g *Generator) genTypeEncoder(t reflect.Type, in string, tags fieldTags, indent int, assumeNonEmpty bool) error {
	ws := strings.Repeat("  ", indent)

	marshalerIface := reflect.TypeOf((*easyjson.Marshaler)(nil)).Elem()
	if reflect.PtrTo(t).Implements(marshalerIface) {
		fmt.Fprintln(g.out, ws+"("+in+").MarshalEasyJSON(out)")
		return nil
	}

	marshalerIface = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	if reflect.PtrTo(t).Implements(marshalerIface) {
		fmt.Fprintln(g.out, ws+"out.Raw( ("+in+").MarshalJSON() )")
		return nil
	}

	marshalerIface = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	if reflect.PtrTo(t).Implements(marshalerIface) {
		fmt.Fprintln(g.out, ws+"out.RawText( ("+in+").MarshalText() )")
		return nil
	}

	err := g.genTypeEncoderNoCheck(t, in, tags, indent, assumeNonEmpty)
	return err
}

// returns true if the type t implements one of the custom marshaler interfaces
func hasCustomMarshaler(t reflect.Type) bool {
	t = reflect.PtrTo(t)
	return t.Implements(reflect.TypeOf((*easyjson.Marshaler)(nil)).Elem()) ||
		t.Implements(reflect.TypeOf((*json.Marshaler)(nil)).Elem()) ||
		t.Implements(reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem())
}

// genTypeEncoderNoCheck generates code that encodes in of type t into the writer.
func (g *Generator) genTypeEncoderNoCheck(t reflect.Type, in string, tags fieldTags, indent int, assumeNonEmpty bool) error {
	ws := strings.Repeat("  ", indent)

	// Check whether type is primitive, needs to be done after interface check.
	if enc := primitiveStringEncoders[t.Kind()]; enc != "" && tags.asString {
		fmt.Fprintf(g.out, ws+enc+"\n", in)
		return nil
	}

	if enc := primitiveEncoders[t.Kind()]; enc != "" {
		fmt.Fprintf(g.out, ws+enc+"\n", in)
		return nil
	}

	switch t.Kind() {
	case reflect.Slice:
		elem := t.Elem()
		iVar := g.uniqueVarName()
		vVar := g.uniqueVarName()

		if t.Elem().Kind() == reflect.Uint8 && elem.Name() == "uint8" {
			if g.simpleBytes {
				fmt.Fprintln(g.out, ws+"out.String(string("+in+"))")
			} else {
				fmt.Fprintln(g.out, ws+"out.Base64Bytes("+in+")")
			}
		} else {
			if !assumeNonEmpty {
				fmt.Fprintln(g.out, ws+"if "+in+" == nil && (out.Flags & jwriter.NilSliceAsEmpty) == 0 {")
				fmt.Fprintln(g.out, ws+`  out.RawString("null")`)
				fmt.Fprintln(g.out, ws+"} else {")
			} else {
				fmt.Fprintln(g.out, ws+"{")
			}
			fmt.Fprintln(g.out, ws+"  out.RawByte('[')")
			fmt.Fprintln(g.out, ws+"  for "+iVar+", "+vVar+" := range "+in+" {")
			fmt.Fprintln(g.out, ws+"    if "+iVar+" > 0 {")
			fmt.Fprintln(g.out, ws+"      out.RawByte(',')")
			fmt.Fprintln(g.out, ws+"    }")

			if err := g.genTypeEncoder(elem, vVar, tags, indent+2, false); err != nil {
				return err
			}

			fmt.Fprintln(g.out, ws+"  }")
			fmt.Fprintln(g.out, ws+"  out.RawByte(']')")
			fmt.Fprintln(g.out, ws+"}")
		}

	case reflect.Array:
		elem := t.Elem()
		iVar := g.uniqueVarName()

		if t.Elem().Kind() == reflect.Uint8 && elem.Name() == "uint8" {
			if g.simpleBytes {
				fmt.Fprintln(g.out, ws+"out.String(string("+in+"[:]))")
			} else {
				fmt.Fprintln(g.out, ws+"out.Base64Bytes("+in+"[:])")
			}
		} else {
			fmt.Fprintln(g.out, ws+"out.RawByte('[')")
			fmt.Fprintln(g.out, ws+"for "+iVar+" := range "+in+" {")
			fmt.Fprintln(g.out, ws+"  if "+iVar+" > 0 {")
			fmt.Fprintln(g.out, ws+"    out.RawByte(',')")
			fmt.Fprintln(g.out, ws+"  }")

			if err := g.genTypeEncoder(elem, "("+in+")["+iVar+"]", tags, indent+1, false); err != nil {
				return err
			}

			fmt.Fprintln(g.out, ws+"}")
			fmt.Fprintln(g.out, ws+"out.RawByte(']')")
		}

	case reflect.Struct:
		enc := g.getEncoderName(t)
		g.addType(t)

		fmt.Fprintln(g.out, ws+enc+"(out, "+in+")")

	case reflect.Ptr:
		if !assumeNonEmpty {
			fmt.Fprintln(g.out, ws+"if "+in+" == nil {")
			fmt.Fprintln(g.out, ws+`  out.RawString("null")`)
			fmt.Fprintln(g.out, ws+"} else {")
		}

		if err := g.genTypeEncoder(t.Elem(), "*"+in, tags, indent+1, false); err != nil {
			return err
		}

		if !assumeNonEmpty {
			fmt.Fprintln(g.out, ws+"}")
		}

	case reflect.Map:
		key := t.Key()
		keyEnc, ok := primitiveStringEncoders[key.Kind()]
		if !ok && !hasCustomMarshaler(key) {
			return fmt.Errorf("map key type %v not supported: only string and integer keys and types implementing Marshaler interfaces are allowed", key)
		} // else assume the caller knows what they are doing and that the custom marshaler performs the translation from the key type to a string or integer
		tmpVar := g.uniqueVarName()

		if !assumeNonEmpty {
			fmt.Fprintln(g.out, ws+"if "+in+" == nil && (out.Flags & jwriter.NilMapAsEmpty) == 0 {")
			fmt.Fprintln(g.out, ws+"  out.RawString(`null`)")
			fmt.Fprintln(g.out, ws+"} else {")
		} else {
			fmt.Fprintln(g.out, ws+"{")
		}
		fmt.Fprintln(g.out, ws+"  out.RawByte('{')")
		fmt.Fprintln(g.out, ws+"  "+tmpVar+"First := true")
		fmt.Fprintln(g.out, ws+"  for "+tmpVar+"Name, "+tmpVar+"Value := range "+in+" {")
		fmt.Fprintln(g.out, ws+"    if "+tmpVar+"First { "+tmpVar+"First = false } else { out.RawByte(',') }")

		// NOTE: extra check for TextMarshaler. It overrides default methods.
		if reflect.PtrTo(key).Implements(reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()) {
			fmt.Fprintln(g.out, ws+"    "+fmt.Sprintf("out.RawBytesString(("+tmpVar+"Name).MarshalText()"+")"))
		} else if keyEnc != "" {
			fmt.Fprintln(g.out, ws+"    "+fmt.Sprintf(keyEnc, tmpVar+"Name"))
		} else {
			if err := g.genTypeEncoder(key, tmpVar+"Name", tags, indent+2, false); err != nil {
				return err
			}
		}

		fmt.Fprintln(g.out, ws+"    out.RawByte(':')")

		if err := g.genTypeEncoder(t.Elem(), tmpVar+"Value", tags, indent+2, false); err != nil {
			return err
		}

		fmt.Fprintln(g.out, ws+"  }")
		fmt.Fprintln(g.out, ws+"  out.RawByte('}')")
		fmt.Fprintln(g.out, ws+"}")

	case reflect.Interface:
		if t.NumMethod() != 0 {
			if g.interfaceIsEasyjsonMarshaller(t) {
				fmt.Fprintln(g.out, ws+in+".MarshalEasyJSON(out)")
			} else if g.interfaceIsJSONMarshaller(t) {
				fmt.Fprintln(g.out, ws+"if m, ok := "+in+".(easyjson.Marshaler); ok {")
				fmt.Fprintln(g.out, ws+"  m.MarshalEasyJSON(out)")
				fmt.Fprintln(g.out, ws+"} else {")
				fmt.Fprintln(g.out, ws+in+".MarshalJSON()")
				fmt.Fprintln(g.out, ws+"}")
			} else {
				return fmt.Errorf("interface type %v not supported: only interface{} and interfaces that implement json or easyjson Marshaling are allowed", t)
			}
		} else {
			fmt.Fprintln(g.out, ws+"if m, ok := "+in+".(easyjson.Marshaler); ok {")
			fmt.Fprintln(g.out, ws+"  m.MarshalEasyJSON(out)")
			fmt.Fprintln(g.out, ws+"} else if m, ok := "+in+".(json.Marshaler); ok {")
			fmt.Fprintln(g.out, ws+"  out.Raw(m.MarshalJSON())")
			fmt.Fprintln(g.out, ws+"} else {")
			fmt.Fprintln(g.out, ws+"  out.Raw(json.Marshal("+in+"))")
			fmt.Fprintln(g.out, ws+"}")
		}
	default:
		return fmt.Errorf("don't know how to encode %v", t)
	}
	return nil
}

func (g *Generator) interfaceIsEasyjsonMarshaller(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*easyjson.Marshaler)(nil)).Elem())
}

func (g *Generator) interfaceIsJSONMarshaller(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*json.Marshaler)(nil)).Elem())
}

func (g *Generator) notEmptyCheck(t reflect.Type, v string) string {
	optionalIface := reflect.TypeOf((*easyjson.Optional)(nil)).Elem()
	if reflect.PtrTo(t).Implements(optionalIface) {
		return "(" + v + ").IsDefined()"
	}

	switch t.Kind() {
	case reflect.Slice, reflect.Map:
		return "len(" + v + ") != 0"
	case reflect.Interface, reflect.Ptr:
		return v + " != nil"
	case reflect.Bool:
		return v
	case reflect.String:
		return v + ` != ""`
	case reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:

		return v + " != 0"

	default:
		// note: Array types don't have a useful empty value
		return "true"
	}
}

func (g *Generator) genStructFieldEncoder(t reflect.Type, f reflect.StructField, first, firstCondition bool) (bool, error) {
	jsonName := g.fieldNamer.GetJSONFieldName(t, f)
	tags := parseFieldTags(f)

	if tags.omit {
		return firstCondition, nil
	}

	toggleFirstCondition := firstCondition

	noOmitEmpty := (!tags.omitEmpty && !g.omitEmpty) || tags.noOmitEmpty
	if noOmitEmpty {
		fmt.Fprintln(g.out, "  {")
		toggleFirstCondition = false
	} else {
		fmt.Fprintln(g.out, "  if", g.notEmptyCheck(f.Type, "in."+f.Name), "{")
		// can be any in runtime, so toggleFirstCondition stay as is
	}

	if firstCondition {
		fmt.Fprintf(g.out, "    const prefix string = %q\n", ","+strconv.Quote(jsonName)+":")
		if first {
			if !noOmitEmpty {
				fmt.Fprintln(g.out, "      first = false")
			}
			fmt.Fprintln(g.out, "      out.RawString(prefix[1:])")
		} else {
			fmt.Fprintln(g.out, "    if first {")
			fmt.Fprintln(g.out, "      first = false")
			fmt.Fprintln(g.out, "      out.RawString(prefix[1:])")
			fmt.Fprintln(g.out, "    } else {")
			fmt.Fprintln(g.out, "      out.RawString(prefix)")
			fmt.Fprintln(g.out, "    }")
		}
	} else {
		fmt.Fprintf(g.out, "    const prefix string = %q\n", ","+strconv.Quote(jsonName)+":")
		fmt.Fprintln(g.out, "    out.RawString(prefix)")
	}

	if err := g.genTypeEncoder(f.Type, "in."+f.Name, tags, 2, !noOmitEmpty); err != nil {
		return toggleFirstCondition, err
	}
	fmt.Fprintln(g.out, "  }")
	return toggleFirstCondition, nil
}

func (g *Generator) genEncoder(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return g.genSliceArrayMapEncoder(t)
	default:
		return g.genStructEncoder(t)
	}
}

func (g *Generator) genSliceArrayMapEncoder(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
	default:
		return fmt.Errorf("cannot generate encoder/decoder for %v, not a slice/array/map type", t)
	}

	fname := g.getEncoderName(t)
	typ := g.getType(t)

	fmt.Fprintln(g.out, "func "+fname+"(out *jwriter.Writer, in "+typ+") {")
	err := g.genTypeEncoderNoCheck(t, "in", fieldTags{}, 1, false)
	if err != nil {
		return err
	}
	fmt.Fprintln(g.out, "}")
	return nil
}

func (g *Generator) genStructEncoder(t reflect.Type) error {
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("cannot generate encoder/decoder for %v, not a struct type", t)
	}

	fname := g.getEncoderName(t)
	typ := g.getType(t)

	fmt.Fprintln(g.out, "func "+fname+"(out *jwriter.Writer, in "+typ+") {")
	fmt.Fprintln(g.out, "  out.RawByte('{')")
	fmt.Fprintln(g.out, "  first := true")
	fmt.Fprintln(g.out, "  _ = first")

	fs, err := getStructFields(t)
	if err != nil {
		return fmt.Errorf("cannot generate encoder for %v: %v", t, err)
	}

	firstCondition := true
	for i, f := range fs {
		firstCondition, err = g.genStructFieldEncoder(t, f, i == 0, firstCondition)

		if err != nil {
			return err
		}
	}

	if hasUnknownsMarshaler(t) {
		if !firstCondition {
			fmt.Fprintln(g.out, "  in.MarshalUnknowns(out, false)")
		} else {
			fmt.Fprintln(g.out, "  in.MarshalUnknowns(out, first)")
		}
	}

	fmt.Fprintln(g.out, "  out.RawByte('}')")
	fmt.Fprintln(g.out, "}")

	return nil
}

func (g *Generator) genStructMarshaler(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
	default:
		return fmt.Errorf("cannot generate encoder/decoder for %v, not a struct/slice/array/map type", t)
	}

	fname := g.getEncoderName(t)
	typ := g.getType(t)

	if !g.noStdMarshalers {
		fmt.Fprintln(g.out, "// MarshalJSON supports json.Marshaler interface")
		fmt.Fprintln(g.out, "func (v "+typ+") MarshalJSON() ([]byte, error) {")
		fmt.Fprintln(g.out, "  w := jwriter.Writer{}")
		fmt.Fprintln(g.out, "  "+fname+"(&w, v)")
		fmt.Fprintln(g.out, "  return w.Buffer.BuildBytes(), w.Error")
		fmt.Fprintln(g.out, "}")
	}

	fmt.Fprintln(g.out, "// MarshalEasyJSON supports easyjson.Marshaler interface")
	fmt.Fprintln(g.out, "func (v "+typ+") MarshalEasyJSON(w *jwriter.Writer) {")
	fmt.Fprintln(g.out, "  "+fname+"(w, v)")
	fmt.Fprintln(g.out, "}")

	return nil
}
