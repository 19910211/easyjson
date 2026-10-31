package jwriter

import (
	"math"
	"strings"
	"testing"
)

func TestFloatRejectsNaNAndInf(t *testing.T) {
	for _, n := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		w := Writer{}
		w.Float64(n)
		if w.Error == nil || !strings.Contains(w.Error.Error(), "unsupported value") {
			t.Fatalf("%v: %v", n, w.Error)
		}
	}
}

func TestRawTextEmptyIsEmptyString(t *testing.T) {
	w := Writer{}
	w.RawText(nil, nil)
	b, err := w.BuildBytes()
	if err != nil || string(b) != `""` {
		t.Fatalf("%q %v", b, err)
	}

	w = Writer{}
	w.RawBytesString(nil, nil)
	b, err = w.BuildBytes()
	if err != nil || string(b) != `""` {
		t.Fatalf("RawBytesString %q %v", b, err)
	}
}
