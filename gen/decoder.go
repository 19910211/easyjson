package gen

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/19910211/easyjson"
)

// Target this byte size for initial slice allocation to reduce garbage collection.
const minSliceBytes = 64

func (g *Generator) getDecoderName(t reflect.Type) string {
	return g.functionName("decode", t)
}

var primitiveDecoders = map[reflect.Kind]string{
	reflect.String:  "in.String()",
	reflect.Bool:    "in.Bool()",
	reflect.Int:     "in.Int()",
	reflect.Int8:    "in.Int8()",
	reflect.Int16:   "in.Int16()",
	reflect.Int32:   "in.Int32()",
	reflect.Int64:   "in.Int64()",
	reflect.Uint:    "in.Uint()",
	reflect.Uint8:   "in.Uint8()",
	reflect.Uint16:  "in.Uint16()",
	reflect.Uint32:  "in.Uint32()",
	reflect.Uint64:  "in.Uint64()",
	reflect.Float32: "in.Float32()",
	reflect.Float64: "in.Float64()",
}

var primitiveStringDecoders = map[reflect.Kind]string{
	reflect.String:  "in.String()",
	reflect.Int:     "in.IntStr()",
	reflect.Int8:    "in.Int8Str()",
	reflect.Int16:   "in.Int16Str()",
	reflect.Int32:   "in.Int32Str()",
	reflect.Int64:   "in.Int64Str()",
	reflect.Uint:    "in.UintStr()",
	reflect.Uint8:   "in.Uint8Str()",
	reflect.Uint16:  "in.Uint16Str()",
	reflect.Uint32:  "in.Uint32Str()",
	reflect.Uint64:  "in.Uint64Str()",
	reflect.Uintptr: "in.UintptrStr()",
	reflect.Float32: "in.Float32Str()",
	reflect.Float64: "in.Float64Str()",
}

var customDecoders = map[string]string{
	"json.Number": "in.JsonNumber()",
}

// genTypeDecoder generates decoding code for the type t, but uses unmarshaler interface if implemented by t.
func (g *Generator) genTypeDecoder(t reflect.Type, out string, tags fieldTags, indent int) error {
	ws := strings.Repeat("  ", indent)

	unmarshalerIface := reflect.TypeOf((*easyjson.Unmarshaler)(nil)).Elem()
	if reflect.PtrTo(t).Implements(unmarshalerIface) {
		fmt.Fprintln(g.out, ws+"if in.IsNull() {")
		fmt.Fprintln(g.out, ws+"  in.Skip()")
		fmt.Fprintln(g.out, ws+"} else {")
		fmt.Fprintln(g.out, ws+"  ("+out+").UnmarshalEasyJSON(in)")
		fmt.Fprintln(g.out, ws+"}")
		return nil
	}

	unmarshalerIface = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	if reflect.PtrTo(t).Implements(unmarshalerIface) {
		fmt.Fprintln(g.out, ws+"if in.IsNull() {")
		fmt.Fprintln(g.out, ws+"  in.Skip()")
		fmt.Fprintln(g.out, ws+"} else {")
		fmt.Fprintln(g.out, ws+"  if data := in.Raw(); in.Ok() {")
		fmt.Fprintln(g.out, ws+"    in.AddError( ("+out+").UnmarshalJSON(data) )")
		fmt.Fprintln(g.out, ws+"  }")
		fmt.Fprintln(g.out, ws+"}")
		return nil
	}

	unmarshalerIface = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	if reflect.PtrTo(t).Implements(unmarshalerIface) {
		fmt.Fprintln(g.out, ws+"if in.IsNull() {")
		fmt.Fprintln(g.out, ws+"  in.Skip()")
		fmt.Fprintln(g.out, ws+"} else {")
		fmt.Fprintln(g.out, ws+"  if data := in.UnsafeBytes(); in.Ok() {")
		fmt.Fprintln(g.out, ws+"    in.AddError( ("+out+").UnmarshalText(data) )")
		fmt.Fprintln(g.out, ws+"  }")
		fmt.Fprintln(g.out, ws+"}")
		return nil
	}

	err := g.genTypeDecoderNoCheck(t, out, tags, indent)
	return err
}

// returns true if the type t implements one of the custom unmarshaler interfaces
func hasCustomUnmarshaler(t reflect.Type) bool {
	t = reflect.PtrTo(t)
	return t.Implements(reflect.TypeOf((*easyjson.Unmarshaler)(nil)).Elem()) ||
		t.Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()) ||
		t.Implements(reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem())
}

func hasUnknownsUnmarshaler(t reflect.Type) bool {
	t = reflect.PtrTo(t)
	return t.Implements(reflect.TypeOf((*easyjson.UnknownsUnmarshaler)(nil)).Elem())
}

func hasUnknownsMarshaler(t reflect.Type) bool {
	t = reflect.PtrTo(t)
	return t.Implements(reflect.TypeOf((*easyjson.UnknownsMarshaler)(nil)).Elem())
}

// genTypeDecoderNoCheck generates decoding code for the type t.
func (g *Generator) genTypeDecoderNoCheck(t reflect.Type, out string, tags fieldTags, indent int) error {
	ws := strings.Repeat("  ", indent)
	// Check whether type is primitive, needs to be done after interface check.
	if dec := customDecoders[t.String()]; dec != "" {
		fmt.Fprintln(g.out, ws+"if in.IsNull() {")
		fmt.Fprintln(g.out, ws+"  in.Skip()")
		fmt.Fprintln(g.out, ws+"} else {")
		fmt.Fprintln(g.out, ws+"  "+out+" = "+dec)
		fmt.Fprintln(g.out, ws+"}")
		return nil
	} else if dec := primitiveStringDecoders[t.Kind()]; dec != "" && tags.asString {
		if tags.intern && t.Kind() == reflect.String {
			dec = "in.StringIntern()"
		}
		fmt.Fprintln(g.out, ws+"if in.IsNull() {")
		fmt.Fprintln(g.out, ws+"  in.Skip()")
		fmt.Fprintln(g.out, ws+"} else {")
		fmt.Fprintln(g.out, ws+"  "+out+" = "+g.getType(t)+"("+dec+")")
		fmt.Fprintln(g.out, ws+"}")
		return nil
	} else if dec := primitiveDecoders[t.Kind()]; dec != "" {
		if tags.intern && t.Kind() == reflect.String {
			dec = "in.StringIntern()"
		}
		if tags.noCopy && t.Kind() == reflect.String {
			dec = "in.UnsafeString()"
		}
		fmt.Fprintln(g.out, ws+"if in.IsNull() {")
		fmt.Fprintln(g.out, ws+"  in.Skip()")
		fmt.Fprintln(g.out, ws+"} else {")
		fmt.Fprintln(g.out, ws+"  "+out+" = "+g.getType(t)+"("+dec+")")
		fmt.Fprintln(g.out, ws+"}")
		return nil
	}

	switch t.Kind() {
	case reflect.Slice:
		tmpVar := g.uniqueVarName()
		elem := t.Elem()

		if elem.Kind() == reflect.Uint8 && elem.Name() == "uint8" {
			fmt.Fprintln(g.out, ws+"if in.IsNull() {")
			fmt.Fprintln(g.out, ws+"  in.Skip()")
			fmt.Fprintln(g.out, ws+"  "+out+" = nil")
			fmt.Fprintln(g.out, ws+"} else {")
			if g.simpleBytes {
				fmt.Fprintln(g.out, ws+"  "+out+" = []byte(in.String())")
			} else {
				fmt.Fprintln(g.out, ws+"  "+out+" = in.Bytes()")
			}

			fmt.Fprintln(g.out, ws+"}")

		} else {

			capacity := 1
			if elem.Size() > 0 {
				capacity = minSliceBytes / int(elem.Size())
			}

			fmt.Fprintln(g.out, ws+"if in.IsNull() {")
			fmt.Fprintln(g.out, ws+"  in.Skip()")
			fmt.Fprintln(g.out, ws+"  "+out+" = nil")
			fmt.Fprintln(g.out, ws+"} else {")
			fmt.Fprintln(g.out, ws+"  in.Delim('[')")
			fmt.Fprintln(g.out, ws+"  if "+out+" == nil {")
			fmt.Fprintln(g.out, ws+"    if !in.IsDelim(']') {")
			fmt.Fprintln(g.out, ws+"      "+out+" = make("+g.getType(t)+", 0, "+fmt.Sprint(capacity)+")")
			fmt.Fprintln(g.out, ws+"    } else {")
			fmt.Fprintln(g.out, ws+"      "+out+" = "+g.getType(t)+"{}")
			fmt.Fprintln(g.out, ws+"    }")
			fmt.Fprintln(g.out, ws+"  } else { ")
			fmt.Fprintln(g.out, ws+"    "+out+" = ("+out+")[:0]")
			fmt.Fprintln(g.out, ws+"  }")
			fmt.Fprintln(g.out, ws+"  for !in.IsDelim(']') {")
			fmt.Fprintln(g.out, ws+"    var "+tmpVar+" "+g.getType(elem))

			if err := g.genTypeDecoder(elem, tmpVar, tags, indent+2); err != nil {
				return err
			}

			if tags.sliceValOmitempty {
				if elem.Kind() == reflect.String {
					fmt.Fprintln(g.out, ws+"        if "+tmpVar+` != "" {`)
					fmt.Fprintln(g.out, ws+"            "+out+" = append("+out+", "+tmpVar+")")
					fmt.Fprintln(g.out, ws+"            }")
				} else if elem.Kind() == reflect.Ptr {
					fmt.Fprintln(g.out, ws+"        if "+tmpVar+` != nil {`)
					fmt.Fprintln(g.out, ws+"            "+out+" = append("+out+", "+tmpVar+")")
					fmt.Fprintln(g.out, ws+"            }")
				} else {
					fmt.Fprintln(g.out, ws+"        "+out+" = append("+out+", "+tmpVar+")")
				}
			} else {
				fmt.Fprintln(g.out, ws+"        "+out+" = append("+out+", "+tmpVar+")")
			}
			fmt.Fprintln(g.out, ws+"    in.WantComma()")
			fmt.Fprintln(g.out, ws+"  }")
			fmt.Fprintln(g.out, ws+"  in.Delim(']')")
			fmt.Fprintln(g.out, ws+"}")
		}

	case reflect.Array:
		iterVar := g.uniqueVarName()
		elem := t.Elem()

		if elem.Kind() == reflect.Uint8 && elem.Name() == "uint8" {
			fmt.Fprintln(g.out, ws+"if in.IsNull() {")
			fmt.Fprintln(g.out, ws+"  in.Skip()")
			fmt.Fprintln(g.out, ws+"} else {")
			fmt.Fprintln(g.out, ws+"  copy("+out+"[:], in.Bytes())")
			fmt.Fprintln(g.out, ws+"}")

		} else {

			length := t.Len()

			fmt.Fprintln(g.out, ws+"if in.IsNull() {")
			fmt.Fprintln(g.out, ws+"  in.Skip()")
			fmt.Fprintln(g.out, ws+"} else {")
			fmt.Fprintln(g.out, ws+"  in.Delim('[')")
			fmt.Fprintln(g.out, ws+"  "+iterVar+" := 0")
			fmt.Fprintln(g.out, ws+"  for !in.IsDelim(']') {")
			fmt.Fprintln(g.out, ws+"    if "+iterVar+" < "+fmt.Sprint(length)+" {")

			if err := g.genTypeDecoder(elem, "("+out+")["+iterVar+"]", tags, indent+3); err != nil {
				return err
			}

			fmt.Fprintln(g.out, ws+"      "+iterVar+"++")
			fmt.Fprintln(g.out, ws+"    } else {")
			fmt.Fprintln(g.out, ws+"      in.SkipRecursive()")
			fmt.Fprintln(g.out, ws+"    }")
			fmt.Fprintln(g.out, ws+"    in.WantComma()")
			fmt.Fprintln(g.out, ws+"  }")
			fmt.Fprintln(g.out, ws+"  in.Delim(']')")
			fmt.Fprintln(g.out, ws+"}")
		}

	case reflect.Struct:
		dec := g.getDecoderName(t)
		g.addType(t)

		if len(out) > 0 && out[0] == '*' {
			// NOTE: In order to remove an extra reference to a pointer
			fmt.Fprintln(g.out, ws+dec+"(in, "+out[1:]+")")
		} else {
			fmt.Fprintln(g.out, ws+dec+"(in, &"+out+")")
		}

	case reflect.Ptr:
		fmt.Fprintln(g.out, ws+"if in.IsNull() {")
		fmt.Fprintln(g.out, ws+"  in.Skip()")
		fmt.Fprintln(g.out, ws+"  "+out+" = nil")
		fmt.Fprintln(g.out, ws+"} else {")
		fmt.Fprintln(g.out, ws+"  if "+out+" == nil {")
		if tags.newStruct {
			fmt.Fprintln(g.out, ws+"    "+out+" = new"+g.getType(t.Elem())+"()")
		} else if tags.NewStruct {
			fmt.Fprintln(g.out, ws+"    "+out+" = New"+g.getType(t.Elem())+"()")
		} else {
			fmt.Fprintln(g.out, ws+"    "+out+" = new("+g.getType(t.Elem())+")")
		}

		fmt.Fprintln(g.out, ws+"  }")

		if err := g.genTypeDecoder(t.Elem(), "*"+out, tags, indent+1); err != nil {
			return err
		}

		fmt.Fprintln(g.out, ws+"}")

	case reflect.Map:
		key := t.Key()
		keyDec, ok := primitiveStringDecoders[key.Kind()]
		if !ok && !hasCustomUnmarshaler(key) {
			return fmt.Errorf("map type %v not supported: only string and integer keys and types implementing json.Unmarshaler are allowed", key)
		} // else assume the caller knows what they are doing and that the custom unmarshaler performs the translation from string or integer keys to the key type
		elem := t.Elem()
		tmpVar := g.uniqueVarName()
		keepEmpty := tags.required || tags.noOmitEmpty || (!g.omitEmpty && !tags.omitEmpty)

		fmt.Fprintln(g.out, ws+"if in.IsNull() {")
		fmt.Fprintln(g.out, ws+"  in.Skip()")
		fmt.Fprintln(g.out, ws+"} else {")
		fmt.Fprintln(g.out, ws+"  in.Delim('{')")
		if !keepEmpty {
			fmt.Fprintln(g.out, ws+"  if !in.IsDelim('}') {")
		}
		fmt.Fprintln(g.out, ws+"  "+out+" = make("+g.getType(t)+")")
		if !keepEmpty {
			fmt.Fprintln(g.out, ws+"  } else {")
			fmt.Fprintln(g.out, ws+"  "+out+" = nil")
			fmt.Fprintln(g.out, ws+"  }")
		}

		fmt.Fprintln(g.out, ws+"  for !in.IsDelim('}') {")
		// NOTE: extra check for TextUnmarshaler. It overrides default methods.
		if reflect.PtrTo(key).Implements(reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()) {
			fmt.Fprintln(g.out, ws+"    var key "+g.getType(key))
			fmt.Fprintln(g.out, ws+"if data := in.UnsafeBytes(); in.Ok() {")
			fmt.Fprintln(g.out, ws+"  in.AddError(key.UnmarshalText(data) )")
			fmt.Fprintln(g.out, ws+"}")
		} else if keyDec != "" {
			fmt.Fprintln(g.out, ws+"    key := "+g.getType(key)+"("+keyDec+")")
		} else {
			fmt.Fprintln(g.out, ws+"    var key "+g.getType(key))
			if err := g.genTypeDecoder(key, "key", tags, indent+2); err != nil {
				return err
			}
		}

		fmt.Fprintln(g.out, ws+"    in.WantColon()")
		fmt.Fprintln(g.out, ws+"    var "+tmpVar+" "+g.getType(elem))

		if err := g.genTypeDecoder(elem, tmpVar, tags, indent+2); err != nil {
			return err
		}

		fmt.Fprintln(g.out, ws+"    ("+out+")[key] = "+tmpVar)
		fmt.Fprintln(g.out, ws+"    in.WantComma()")
		fmt.Fprintln(g.out, ws+"  }")
		fmt.Fprintln(g.out, ws+"  in.Delim('}')")
		fmt.Fprintln(g.out, ws+"}")

	case reflect.Interface:
		if t.NumMethod() != 0 {
			if g.interfaceIsEasyjsonUnmarshaller(t) {
				fmt.Fprintln(g.out, ws+out+".UnmarshalEasyJSON(in)")
			} else if g.interfaceIsJsonUnmarshaller(t) {
				fmt.Fprintln(g.out, ws+out+".UnmarshalJSON(in.Raw())")
			} else {
				return fmt.Errorf("interface type %v not supported: only interface{} and easyjson/json Unmarshaler are allowed", t)
			}
		} else {
			fmt.Fprintln(g.out, ws+"if m, ok := "+out+".(easyjson.Unmarshaler); ok {")
			fmt.Fprintln(g.out, ws+"m.UnmarshalEasyJSON(in)")
			fmt.Fprintln(g.out, ws+"} else if m, ok := "+out+".(json.Unmarshaler); ok {")
			fmt.Fprintln(g.out, ws+"_ = m.UnmarshalJSON(in.Raw())")
			fmt.Fprintln(g.out, ws+"} else {")
			fmt.Fprintln(g.out, ws+"  "+out+" = in.Interface()")
			fmt.Fprintln(g.out, ws+"}")
		}
	default:
		return fmt.Errorf("don't know how to decode %v", t)
	}
	return nil
}

func (g *Generator) interfaceIsEasyjsonUnmarshaller(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*easyjson.Unmarshaler)(nil)).Elem())
}

func (g *Generator) interfaceIsJsonUnmarshaller(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem())
}

func (g *Generator) genStructFieldDecoder(t reflect.Type, f reflect.StructField) error {
	jsonName := g.fieldNamer.GetJSONFieldName(t, f)
	tags := parseFieldTags(f)

	if tags.omit {
		return nil
	}
	if tags.intern && tags.noCopy {
		return errors.New("Mutually exclusive tags are specified: 'intern' and 'nocopy'")
	}

	fmt.Fprintf(g.out, "    case %q:\n", jsonName)
	if err := g.genTypeDecoder(f.Type, "out."+f.Name, tags, 3); err != nil {
		return err
	}

	if tags.required {
		fmt.Fprintf(g.out, "%sSet = true\n", f.Name)
	}

	return nil
}

func (g *Generator) genRequiredFieldSet(t reflect.Type, f reflect.StructField) {
	tags := parseFieldTags(f)

	if !tags.required {
		return
	}

	fmt.Fprintf(g.out, "var %sSet bool\n", f.Name)
}

func (g *Generator) genRequiredFieldCheck(t reflect.Type, f reflect.StructField) {
	jsonName := g.fieldNamer.GetJSONFieldName(t, f)
	tags := parseFieldTags(f)

	if !tags.required {
		return
	}

	g.imports["fmt"] = "fmt"

	fmt.Fprintf(g.out, "if !%sSet {\n", f.Name)
	fmt.Fprintf(g.out, "    in.AddError(fmt.Errorf(\"key '%s' is required\"))\n", jsonName)
	fmt.Fprintf(g.out, "}\n")
}

func mergeStructFields(fields1, fields2 []reflect.StructField) (fields []reflect.StructField) {
	used := map[string]bool{}
	for _, f := range fields2 {
		used[f.Name] = true
		fields = append(fields, f)
	}

	for _, f := range fields1 {
		if !used[f.Name] {
			fields = append(fields, f)
		}
	}
	return
}

func getStructFields(t reflect.Type) ([]reflect.StructField, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("got %v; expected a struct", t)
	}

	var efields []reflect.StructField
	var fields []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tags := parseFieldTags(f)
		if !f.Anonymous || tags.name != "" {
			continue
		}

		t1 := f.Type
		if t1.Kind() == reflect.Ptr {
			t1 = t1.Elem()
		}

		if t1.Kind() == reflect.Struct {
			fs, err := getStructFields(t1)
			if err != nil {
				return nil, fmt.Errorf("error processing embedded field: %v", err)
			}
			efields = mergeStructFields(efields, fs)
		} else if (t1.Kind() >= reflect.Bool && t1.Kind() < reflect.Complex128) || t1.Kind() == reflect.String {
			if strings.Contains(f.Name, ".") || unicode.IsUpper([]rune(f.Name)[0]) {
				fields = append(fields, f)
			}
		}
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tags := parseFieldTags(f)
		if f.Anonymous && tags.name == "" {
			continue
		}

		c := []rune(f.Name)[0]
		if unicode.IsUpper(c) {
			fields = append(fields, f)
		}
	}
	return mergeStructFields(efields, fields), nil
}

func (g *Generator) genDecoder(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return g.genSliceArrayDecoder(t)
	default:
		return g.genStructDecoder(t)
	}
}

func (g *Generator) genSliceArrayDecoder(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
	default:
		return fmt.Errorf("cannot generate encoder/decoder for %v, not a slice/array/map type", t)
	}

	fname := g.getDecoderName(t)
	typ := g.getType(t)

	fmt.Fprintln(g.out, "func "+fname+"(in *jlexer.Lexer, out *"+typ+") {")
	fmt.Fprintln(g.out, " isTopLevel := in.IsStart()")
	err := g.genTypeDecoderNoCheck(t, "*out", fieldTags{}, 1)
	if err != nil {
		return err
	}
	fmt.Fprintln(g.out, "  if isTopLevel {")
	fmt.Fprintln(g.out, "    in.Consumed()")
	fmt.Fprintln(g.out, "  }")
	fmt.Fprintln(g.out, "}")

	return nil
}

func (g *Generator) genStructDecoder(t reflect.Type) error {
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("cannot generate encoder/decoder for %v, not a struct type", t)
	}

	fname := g.getDecoderName(t)
	typ := g.getType(t)

	fmt.Fprintln(g.out, "func "+fname+"(in *jlexer.Lexer, out *"+typ+") {")
	fmt.Fprintln(g.out, "  isTopLevel := in.IsStart()")
	fmt.Fprintln(g.out, "  if in.IsNull() {")
	fmt.Fprintln(g.out, "    if isTopLevel {")
	fmt.Fprintln(g.out, "      in.Consumed()")
	fmt.Fprintln(g.out, "    }")
	fmt.Fprintln(g.out, "    in.Skip()")
	fmt.Fprintln(g.out, "    return")
	fmt.Fprintln(g.out, "  }")

	// Init embedded pointer fields.
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.Anonymous || f.Type.Kind() != reflect.Ptr {
			continue
		}
		fmt.Fprintln(g.out, "  out."+f.Name+" = new("+g.getType(f.Type.Elem())+")")
	}

	fs, err := getStructFields(t)
	if err != nil {
		return fmt.Errorf("cannot generate decoder for %v: %v", t, err)
	}

	for _, f := range fs {
		g.genRequiredFieldSet(t, f)
	}

	fmt.Fprintln(g.out, "  in.Delim('{')")
	fmt.Fprintln(g.out, "  for !in.IsDelim('}') {")
	fmt.Fprintf(g.out, "    key := in.UnsafeFieldName(%v)\n", g.skipMemberNameUnescaping)
	fmt.Fprintln(g.out, "    in.WantColon()")

	fmt.Fprintln(g.out, "    switch key {")
	for _, f := range fs {
		if err := g.genStructFieldDecoder(t, f); err != nil {
			return err
		}
	}

	fmt.Fprintln(g.out, "    default:")
	if g.disallowUnknownFields {
		fmt.Fprintln(g.out, `      in.AddError(&jlexer.LexerError{
          Offset: in.GetPos(),
          Reason: "unknown field",
          Data: key,
      })`)
	} else if hasUnknownsUnmarshaler(t) {
		fmt.Fprintln(g.out, "      out.UnmarshalUnknown(in, key)")
	} else {
		fmt.Fprintln(g.out, "      in.SkipRecursive()")
	}
	fmt.Fprintln(g.out, "    }")
	fmt.Fprintln(g.out, "    in.WantComma()")
	fmt.Fprintln(g.out, "  }")
	fmt.Fprintln(g.out, "  in.Delim('}')")
	fmt.Fprintln(g.out, "  if isTopLevel {")
	fmt.Fprintln(g.out, "    in.Consumed()")
	fmt.Fprintln(g.out, "  }")

	for _, f := range fs {
		g.genRequiredFieldCheck(t, f)
	}

	fmt.Fprintln(g.out, "}")

	return nil
}

func (g *Generator) genStructUnmarshaler(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
	default:
		return fmt.Errorf("cannot generate encoder/decoder for %v, not a struct/slice/array/map type", t)
	}

	fname := g.getDecoderName(t)
	typ := g.getType(t)

	if !g.noStdMarshalers {
		fmt.Fprintln(g.out, "// UnmarshalJSON supports json.Unmarshaler interface")
		fmt.Fprintln(g.out, "func (v *"+typ+") UnmarshalJSON(data []byte) error {")
		fmt.Fprintln(g.out, "  r := jlexer.Lexer{Data: data}")
		fmt.Fprintln(g.out, "  "+fname+"(&r, v)")
		fmt.Fprintln(g.out, "  return r.Error()")
		fmt.Fprintln(g.out, "}")
	}

	fmt.Fprintln(g.out, "// UnmarshalEasyJSON supports easyjson.Unmarshaler interface")
	fmt.Fprintln(g.out, "func (v *"+typ+") UnmarshalEasyJSON(l *jlexer.Lexer) {")
	fmt.Fprintln(g.out, "  "+fname+"(l, v)")
	fmt.Fprintln(g.out, "}")

	return nil
}
