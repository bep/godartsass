package functions

import (
	"fmt"
	"reflect"

	sass "github.com/bep/godartsass/internal/embeddedsass"
)

func MarshalValue(input reflect.Value) (returns *sass.Value, err error) {
	returns = new(sass.Value)
	switch c := input.Interface().(type) {
	case string:
		returns.Value = &sass.Value_String_{
			String_: &sass.Value_String{Text: c, Quoted: true},
		}
	case bool:
		var value sass.SingletonValue
		if c {
			value = sass.SingletonValue_TRUE
		} else {
			value = sass.SingletonValue_FALSE
		}
		returns.Value = &sass.Value_Singleton{Singleton: value}
	case nil:
		returns.Value = &sass.Value_Singleton{Singleton: sass.SingletonValue_NULL}
	case Identifier:
		returns.Value = &sass.Value_String_{
			String_: &sass.Value_String{Text: string(c), Quoted: false},
		}
	case *Number:
		returns.Value = &sass.Value_Number_{
			Number: &sass.Value_Number{
				Value:        c.Value,
				Numerators:   c.Numerators,
				Denominators: c.Denominators,
			},
		}
	case *RGBColor:
		returns.Value = &sass.Value_RgbColor_{
			RgbColor: &sass.Value_RgbColor{
				Red:   c.Red,
				Green: c.Green,
				Blue:  c.Blue,
				Alpha: c.Alpha,
			},
		}
	case *HSLColor:
		returns.Value = &sass.Value_HslColor_{
			HslColor: &sass.Value_HslColor{
				Hue:        c.Hue,
				Saturation: c.Saturation,
				Lightness:  c.Lightness,
				Alpha:      c.Alpha,
			},
		}
	case *HWBColor:
		returns.Value = &sass.Value_HwbColor_{
			HwbColor: &sass.Value_HwbColor{
				Hue:       c.Hue,
				Whiteness: c.Whiteness,
				Blackness: c.Blackness,
				Alpha:     c.Alpha,
			},
		}
	case *CompilerFunction:
		returns.Value = &sass.Value_CompilerFunction_{
			CompilerFunction: &sass.Value_CompilerFunction{
				Id: c.ID,
			},
		}
	case *HostFunction:
		returns.Value = &sass.Value_HostFunction_{
			HostFunction: &sass.Value_HostFunction{
				Id:        c.ID,
				Signature: c.Signature,
			},
		}
	default:
		err = fmt.Errorf("unknown value %T", c)
	}
	if err != nil {
		return
	}
	switch input.Kind() {
	case reflect.Array, reflect.Slice:
		var content *sass.Value
		var contents []*sass.Value
		for i := 0; i < input.Len(); i++ {
			if content, err = MarshalValue(input.Index(i)); err != nil {
				return
			}
			contents = append(contents, content)
		}
		returns.Value = &sass.Value_List_{
			List: &sass.Value_List{
				Separator:   sass.ListSeparator_SLASH,
				HasBrackets: true,
				Contents:    contents,
			},
		}
	case reflect.Map:
		iter := input.MapRange()
		var entries []*sass.Value_Map_Entry
		for iter.Next() {
			entry := new(sass.Value_Map_Entry)
			if entry.Key, err = MarshalValue(iter.Key()); err != nil {
				return
			}
			if entry.Value, err = MarshalValue(iter.Value()); err != nil {
				return
			}
			entries = append(entries, entry)
		}
		returns.Value = &sass.Value_Map_{Map: &sass.Value_Map{Entries: entries}}
	}
	return
}
