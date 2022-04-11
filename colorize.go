package main

import (
	"fmt"
	"text/template"

	//	"html/template"
	"os"
	"regexp"

	//	"github.com/mitchellh/colorstring"
	"golang.org/x/sys/windows"
)

type ColorMap map[string]string

var g_ColorValues = map[string]string{
	// Default foreground/background colors
	"default":   "39",
	"_default_": "49",

	// Foreground colors
	"black":         "30",
	"red":           "31",
	"green":         "32",
	"yellow":        "33",
	"blue":          "34",
	"magenta":       "35",
	"cyan":          "36",
	"light_gray":    "37",
	"dark_gray":     "90",
	"light_red":     "91",
	"light_green":   "92",
	"light_yellow":  "93",
	"light_blue":    "94",
	"light_magenta": "95",
	"light_cyan":    "96",
	"white":         "97",

	// Background colors
	"_black_":         "40",
	"_red_":           "41",
	"_green_":         "42",
	"_yellow_":        "43",
	"_blue_":          "44",
	"_magenta_":       "45",
	"_cyan_":          "46",
	"_light_gray_":    "47",
	"_dark_gray_":     "100",
	"_light_red_":     "101",
	"_light_green_":   "102",
	"_light_yellow_":  "103",
	"_light_blue_":    "104",
	"_light_magenta_": "105",
	"_light_cyan_":    "106",
	"_white_":         "107",

	// Attributes
	"bold":       "1",
	"dim":        "2",
	"underline":  "4",
	"blink_slow": "5",
	"blink_fast": "6",
	"invert":     "7",
	"hidden":     "8",

	// Reset to reset everything to their defaults
	"reset":      "0",
	"reset_bold": "21",
}

type Stdio struct {
	in     windows.Handle
	inMode uint32

	out     windows.Handle
	outMode uint32

	err     windows.Handle
	errMode uint32

	vtInputSupported bool
}

var g_Stdio = new(Stdio)

func (stdio *Stdio) initConsole() {
	stdio.in = windows.Handle(os.Stdin.Fd())
	if err := windows.GetConsoleMode(stdio.in, &stdio.inMode); err == nil {
		// Validate that windows.ENABLE_VIRTUAL_TERMINAL_INPUT is supported, but do not set it.
		if err = windows.SetConsoleMode(stdio.in, stdio.inMode|windows.ENABLE_VIRTUAL_TERMINAL_INPUT); err == nil {
			stdio.vtInputSupported = true
		}
		// Unconditionally set the console mode back even on failure because SetConsoleMode
		// remembers invalid bits on input handles.
		windows.SetConsoleMode(stdio.in, stdio.inMode)
	} //else {
	//fmt.Printf("failed to get console mode for stdin: %v\n", err)
	//}

	stdio.out = windows.Handle(os.Stdout.Fd())
	if err := windows.GetConsoleMode(stdio.out, &stdio.outMode); err == nil {
		if err := windows.SetConsoleMode(stdio.out, stdio.outMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err == nil {
			stdio.outMode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		} else {
			windows.SetConsoleMode(stdio.out, stdio.outMode)
		}
	} //else {
	//fmt.Printf("failed to get console mode for stdout: %v\n", err)
	//fmt.Fprintf(os.Stderr, "Not colorizing since redirected to a file\n")
	//}

	stdio.err = windows.Handle(os.Stderr.Fd())
	if err := windows.GetConsoleMode(stdio.err, &stdio.errMode); err == nil {
		if err := windows.SetConsoleMode(stdio.err, stdio.errMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err == nil {
			stdio.errMode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		} else {
			windows.SetConsoleMode(stdio.err, stdio.errMode)
		}
	} //else {
	//fmt.Printf("failed to get console mode for stderr: %v\n", err)
	//}
}

var g_ColorizeTemplate = template.New("Colorization")

func initColorize() {
	g_Stdio.initConsole()
	g_ColorizeTemplate = g_ColorizeTemplate.Funcs(template.FuncMap{"colorize": colorize})
}

func mergeColorMap(colorMap *ColorMap) {
	// merge appColors into g_ColorValues
	if colorMap != nil {
		for k, v := range *colorMap {
			colorValue, ok := getColorValue(v)
			if !ok { // see if it is a reference within the given colorMap
				colorValue, ok = (*colorMap)[v]
				if ok && colorValue != "" {
					colorValue, ok = getColorValue(colorValue)
				}
			}
			// flag if named colors are referenced but missing
			if v != "" && !ok {
				fmt.Printf("*** Bad color name: %v\n", v)
			}
			g_ColorValues[k] = colorValue
		}
	}
}

var g_colorRE = regexp.MustCompile(`([a-zA-Z0-9_\.\-]+)`)

func getColorValue(colorname string) (value string, ok bool) {
	value, ok = g_ColorValues[colorname]
	if !ok {
		if len(colorname) > 0 && 30 <= colorname[0] && colorname[0] <= 39 {
			ok = true
		}
		value = colorname
	}

	return
}

func getColorCodes(colors string) string {
	codes := ""
	for _, color := range g_colorRE.FindAllString(colors, -1) {
		value, ok := getColorValue(color)
		if value != "" && ok {
			codes = codes + fmt.Sprintf("\033[%sm", value)
		}
	}
	return codes
}

func colorize(colors string, s string) string {
	// do not colorize if stdout is redirected to a file
	if (g_Stdio.outMode & windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == 0 {
		return s
	}
	codes := getColorCodes(colors)
	// end with a reset
	return fmt.Sprintf("%s%s\033[0m", codes, s)
}

// func colorizeTpl(tpl string, data interface{}) string {
// 	t, _ := g_ColorizeTemplate.Parse(tpl)
// 	buf := &bytes.Buffer{}
// 	t.Execute(buf, data)
// 	return buf.String()
// }
