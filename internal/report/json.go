package report

import (
	"bufio"
	"encoding/json"
	"io"
	"reflect"
	"strings"
)

// WriteJSON keeps the v1 field order and two-space indentation. Top-level
// slices are encoded one element at a time, so findings/scheduling/limitations
// do not require one formatter buffer proportional to the whole report.
func WriteJSON(w io.Writer, r Report) error {
	b := bufio.NewWriterSize(w, 32*1024)
	enc := json.NewEncoder(fragmentWriter{b})
	write := func(s string) error { _, err := b.WriteString(s); return err }
	value := reflect.ValueOf(r)
	typ := value.Type()
	if err := write("{\n"); err != nil {
		return err
	}
	first := true
	for i := 0; i < value.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")
		field := value.Field(i)
		if len(tag) > 1 && tag[1] == "omitempty" && field.IsZero() {
			continue
		}
		if len(tag) > 1 && tag[1] == "omitempty" && field.Kind() == reflect.Map && field.Len() == 0 {
			continue
		}
		if !first {
			if err := write(",\n"); err != nil {
				return err
			}
		}
		first = false
		if err := write("  \"" + tag[0] + "\": "); err != nil {
			return err
		}
		enc.SetIndent("  ", "  ")
		if field.Kind() != reflect.Slice || field.IsNil() || field.Len() == 0 {
			if err := enc.Encode(field.Interface()); err != nil {
				return err
			}
			continue
		}
		if err := write("[\n"); err != nil {
			return err
		}
		enc.SetIndent("    ", "  ")
		for j := 0; j < field.Len(); j++ {
			if j > 0 {
				if err := write(",\n"); err != nil {
					return err
				}
			}
			if err := write("    "); err != nil {
				return err
			}
			if err := enc.Encode(field.Index(j).Interface()); err != nil {
				return err
			}
		}
		if err := write("\n  ]"); err != nil {
			return err
		}
	}
	if err := write("\n}\n"); err != nil {
		return err
	}
	return b.Flush()
}

// json.Encoder emits a complete JSON value followed by a newline in one Write.
// The outer object supplies separators, so omit only that terminal newline.
// All JSON escaping, sorting of map keys and indentation remain in stdlib.
type fragmentWriter struct{ io.Writer }

func (w fragmentWriter) Write(p []byte) (int, error) {
	size := len(p)
	if size > 0 && p[size-1] == '\n' {
		p = p[:size-1]
	}
	n, err := w.Writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return n, err
	}
	return size, nil
}
