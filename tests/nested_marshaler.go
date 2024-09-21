package tests

import (
	"github.com/19910211/easyjson"
	"github.com/19910211/easyjson/jlexer"
	"github.com/19910211/easyjson/jwriter"
)

//easyjson:json
type NestedMarshaler struct {
	Value  easyjson.MarshalerUnmarshaler
	Value2 int
}

type StructWithMarshaler struct {
	Value int
}

func (s *StructWithMarshaler) UnmarshalEasyJSON(w *jlexer.Lexer) {
	s.Value = w.Int()
}

func (s *StructWithMarshaler) MarshalEasyJSON(w *jwriter.Writer) {
	w.Int(s.Value)
}
