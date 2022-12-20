package functions

import "fmt"

type Identifier string

type Number struct {
	Value        float64
	Numerators   []string
	Denominators []string
}

type RGBColor struct {
	Red   uint32
	Green uint32
	Blue  uint32
	Alpha float64
}

func (c *RGBColor) String() string {
	if c.Alpha == 0 {
		return fmt.Sprintf("rgb(%d, %d, %d)", c.Red, c.Green, c.Blue)
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %0.2f)", c.Red, c.Green, c.Blue, c.Alpha)
}

type HSLColor struct{ Hue, Saturation, Lightness, Alpha float64 }

func (c *HSLColor) String() string {
	if c.Alpha == 0 {
		return fmt.Sprintf("hsl(%f, %f, %f)", c.Hue, c.Saturation, c.Lightness)
	}
	return fmt.Sprintf("hsla(%f, %f, %f, %0.2f)", c.Hue, c.Saturation, c.Lightness, c.Alpha)
}

type HWBColor struct{ Hue, Whiteness, Blackness, Alpha float64 }

func (c *HWBColor) String() string {
	if c.Alpha == 0 {
		return fmt.Sprintf("hwb(%f, %f, %f)", c.Hue, c.Whiteness, c.Blackness)
	}
	return fmt.Sprintf("hwba(%f, %f, %f, %0.2f)", c.Hue, c.Whiteness, c.Blackness, c.Alpha)
}

type CompilerFunction struct{ ID uint32 }

type HostFunction struct {
	ID uint32

	Signature string
}
