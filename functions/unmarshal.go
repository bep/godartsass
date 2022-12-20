package functions

import (
	"fmt"
	"reflect"

	sass "github.com/bep/godartsass/internal/embeddedsass"
)

func UnmarshalValue(input *sass.Value, inType reflect.Type) (returns reflect.Value, err error) {
	if input.GetSingleton() == sass.SingletonValue_NULL {
		returns = reflect.ValueOf((interface{})(nil))
		return
	}
	returns = reflect.New(inType)
	switch inType.Kind() {
	case reflect.String:
		if str := input.GetString_(); str != nil {
			returns = reflect.ValueOf(str.Text)
		}
	case reflect.Bool:
		if x, ok := input.Value.(*sass.Value_Singleton); ok {
			returns.SetBool(x.Singleton == sass.SingletonValue_TRUE)
		}
	case reflect.Array, reflect.Slice:
		var element reflect.Value
		var contents []*sass.Value
		if x, ok := input.Value.(*sass.Value_List_); ok {
			contents = x.List.Contents
		}
		if x, ok := input.Value.(*sass.Value_ArgumentList_); ok && x.ArgumentList.Contents != nil {
			contents = x.ArgumentList.Contents
		}
		for _, content := range contents {
			element, err = UnmarshalValue(content, inType.Elem())
			if err != nil {
				return
			}
			if inType.Kind() == reflect.Slice {
				returns = reflect.AppendSlice(returns, element)
			} else {
				returns = reflect.Append(returns, element)
			}
		}
	case reflect.Map:
		if x, ok := input.Value.(*sass.Value_Map_); ok {
			var key reflect.Value
			var value reflect.Value
			for _, entry := range x.Map.Entries {
				key, err = UnmarshalValue(entry.Key, inType.Key())
				if err != nil {
					return
				}
				value, err = UnmarshalValue(entry.Value, inType.Elem())
				if err != nil {
					return
				}
				returns.SetMapIndex(key, value)
			}
		}
		if x, ok := input.Value.(*sass.Value_ArgumentList_); ok && x.ArgumentList.Keywords != nil {
			var value reflect.Value
			for key, _value := range x.ArgumentList.Keywords {
				value, err = UnmarshalValue(_value, inType.Elem())
				if err != nil {
					return
				}
				returns.SetMapIndex(reflect.ValueOf(key), value)
			}
		}
	case reflect.Interface:
		switch inType {
		case reflect.TypeOf((*Number)(nil)):
			if x, ok := input.Value.(*sass.Value_Number_); ok {
				returns = reflect.ValueOf(&Number{
					Value:        x.Number.Value,
					Numerators:   x.Number.Numerators,
					Denominators: x.Number.Denominators,
				})
			}
		case reflect.TypeOf((*RGBColor)(nil)):
			if x, ok := input.Value.(*sass.Value_RgbColor_); ok {
				returns = reflect.ValueOf(&RGBColor{
					Red:   x.RgbColor.Red,
					Green: x.RgbColor.Green,
					Blue:  x.RgbColor.Blue,
					Alpha: x.RgbColor.Alpha,
				})
			}
		case reflect.TypeOf((*HSLColor)(nil)):
			if x, ok := input.Value.(*sass.Value_HslColor_); ok {
				returns = reflect.ValueOf(&HSLColor{
					Hue:        x.HslColor.Hue,
					Saturation: x.HslColor.Saturation,
					Lightness:  x.HslColor.Lightness,
					Alpha:      x.HslColor.Alpha,
				})
			}
		case reflect.TypeOf((*HWBColor)(nil)):
			if x, ok := input.Value.(*sass.Value_HwbColor_); ok {
				returns = reflect.ValueOf(&HWBColor{
					Hue:       x.HwbColor.Hue,
					Whiteness: x.HwbColor.Whiteness,
					Blackness: x.HwbColor.Blackness,
					Alpha:     x.HwbColor.Alpha,
				})
			}
		}
	}
	if !returns.IsValid() {
		err = fmt.Errorf("unknown value, expected type: %s, input type: %T", inType, input.Value)
	}
	return
}
