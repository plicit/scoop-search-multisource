package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// ----------
var g_ColorMap = ColorMap{
	"app.name":           "yellow",
	"app.name.installed": "light_green",
	"app.version":        "",
	"app.description":    "",
	"source.header":      "light_cyan",
	"source.status":      "",
	"source.summary":     "source.status",
	"bucket.header":      "",
	"totals":             "light_cyan",
	"divider":            "",
	"debug":              "light_red",
	"error":              "light_red",
}

//var g_defaultColorsArg = g_ColorMap.String()

// Parses --source type:path
var g_SourceOptions = SourceRef{
	Cond: "if0",
	Kind: "bucket|buckets|html",
	Path: ":active|:rasa",
}

var g_SearchQueryOptions = SearchQuery{
	Pattern: regexp.MustCompile("(?i)"),
	Fields:  []string{"name", "bins", "description"},
}

var g_SearchQueryOptionsFieldsStr = strings.Join(g_SearchQueryOptions.Fields, ",")

var g_SourceNamedPathsRE = regexp.MustCompile(g_SourceOptions.Path)
var g_SourceFormatRE = regexp.MustCompile(`^(?:(?P<cond>` + g_SourceOptions.Cond + `): )?(?:\[(?P<kind>` + g_SourceOptions.Kind + `)\] )?(?P<path>.*)$`)
var g_SourcePatternHuman = `"<if0:> [<` + g_SourceOptions.Kind + `>] <` + g_SourceOptions.Path + ` or path/url>"`

func (sources *SourceRefs) String() string {
	return fmt.Sprintf("%v", *sources)
}

func (sources *SourceRefs) Set(value string) error {
	//fmt.Printf("%v\n", value)
	source := SourceRef{}

	m := g_SourceFormatRE.FindStringSubmatch(value)
	if m == nil {
		return fmt.Errorf(`Given source does not match the required pattern:
PATTERN: `+g_SourcePatternHuman+`
GIVEN:   %s
`, value)
	}

	source.Cond = m[1]
	source.Kind = m[2]
	source.Path = m[3]

	// check for named path
	m = g_SourceNamedPathsRE.FindStringSubmatch(source.Path)
	if m != nil {
		src := g_Config.NamedSourceRefs[source.Path[1:]]
		// we keep the given condition, if any
		source.Kind = src.Kind
		source.Path = src.Path
	}
	*sources = append(*sources, source)

	return nil
}

func (colors *ColorMap) StringAll(keepEmpty bool) string {
	keyvalstrs := make([]string, 0, len(*colors))
	for key, color := range *colors {
		if keepEmpty || color != "" {
			keyvalstrs = append(keyvalstrs, key+"="+color)
		}
	}
	return strings.Join(keyvalstrs, ";")
}

func (colors *ColorMap) String() string {
	return colors.StringAll(false)
}

func (colors *ColorMap) Set(value string) error {
	//fmt.Printf("%v\n", value)
	err := error(nil)
	// delete map entirely if set to none
	keyvalstrs := strings.Split(value, ";")
	for _, keyvalstr := range keyvalstrs {
		//fmt.Printf("kv: %v\n", keyvalstr)
		// empty map if keyval is none
		if keyvalstr == "none" {
			for k := range *colors {
				delete(*colors, k)
			}
			continue
		}

		kv := strings.Split(keyvalstr, "=")
		if len(kv) == 2 {
			(*colors)[kv[0]] = kv[1]
		} else {
			err = fmt.Errorf("parse error `key=value`: %v", keyvalstr)
		}
	}
	return err
}

type ParsedArgs struct {
	query   SearchQuery
	sources SourceRefs
	fields  string
	cache   float64
	colors  *ColorMap
	linelen int
	hook    bool
	merge   bool
	debug   bool
}

func myUsage() {
	// os.Args[0]
	fmt.Print(colorize("light_cyan", "scoop-search-multisource.exe : Searches Scoop buckets: local, remote, zip, html") + `

` + colorize("yellow", "VERSION") + `: ` + g_Version + `
` + colorize("yellow", "   HOME") + `: https://github.com/plicit/scoop-search-multisource

` + colorize("yellow", "  ALIAS") + `: scoops.exe
` + colorize("yellow", "  USAGE") + `: scoops.exe [OPTIONS] <search-term-or-regexp>
` + colorize("yellow", "   NOTE") + `: search-term is case-insensitive.  Prefix with "(?-i)" for case-sensitive.  See https://pkg.go.dev/regexp/syntax

` + colorize("yellow", "EXAMPLE") + `: scoops.exe -debug -merge=0 -source :active -source "if0: :rasa" -fields "` + g_SearchQueryOptionsFieldsStr + `" "\bqr\b"

` + colorize("yellow", "OPTIONS") + `:

`)

	flag.PrintDefaults()
}

func parseArgs() (args *ParsedArgs) {
	args = &ParsedArgs{}
	args.colors = &g_ColorMap

	flag.Usage = myUsage

	flag.BoolVar(&args.debug, "debug", false, "print debug info (query, fields, sources)")
	flag.BoolVar(&args.hook, "hook", false, "print posh hook code to integrate with scoop")
	flag.BoolVar(&args.merge, "merge", true, "merge the results from all sources into a single output (avoids duplicates)")
	cache_default := 1.0
	flag.Float64Var(&args.cache, "cache", cache_default, "cache duration in days.")
	flag.Var(args.colors, "colors", `colormap for output. "none" deletes the colormap.`)
	flag.IntVar(&args.linelen, "linelen", 120, "max line length for results (trims description)")
	flag.StringVar(&args.fields, "fields", "name,bins", `app manifest fields to search: `+g_SearchQueryOptionsFieldsStr)
	flag.Var(&args.sources, "source", `a specific source to search. (multiple allowed) 

SOURCE FORMAT: `+g_SourcePatternHuman+`
  if0: -- only use the source as a fallback if there were 0 previous matches

EXAMPLES:
  scoops.exe -source "mybucket.zip" -source "if0: :rasa" python
  scoops.exe -source "[html] https://rasa.github.io/scoop-directory/by-score.html" actools
  scoops.exe -source "[bucket] https://github.com/ScoopInstaller/Versions" python
  scoops.exe -source "%USERPROFILE%\scoop\buckets\main" python
`)

	flag.Parse()

	// --hook: print posh hook and exit if requested
	if args.hook {
		fmt.Println(poshHook)
		os.Exit(0)
	}

	// --cache X (in minutes)
	g_CacheDuration = time.Duration(args.cache * float64(24*time.Hour))

	// --source: setup default sources
	if args.sources == nil {
		args.sources = SourceRefs{g_Config.NamedSourceRefs["active"], g_Config.NamedSourceRefs["rasa"]}
	} // else if custom sources are on the command line, then the default fallback is not added

	// <search-term> regexp parser
	query := ""
	remaining := flag.Args() // remaining args
	if len(remaining) > 0 {
		query = remaining[0]
		// prepend case-insensitivity.  This can be overridden by the user by starting their query with "(?-i)"
		query = "(?i)" + query
	}

	re_Query, err := regexp.Compile(query)
	checkWith(err, "Failed to parse search term regexp")

	// <search-term> and --fields
	args.query = SearchQuery{Pattern: re_Query, Fields: strings.Split(args.fields, ",")}

	return
}

//// ----------
//// Flag type to aggregate multiple args with the same keyword
// --source <type>:<value> option
//type StringSlice []string
//
//func (slice *StringSlice) String() string {
//	return fmt.Sprintf("%v", *slice)
//}
//
//func (slice *StringSlice) Set(value string) error {
//	//fmt.Printf("%v\n", value)
//	*slice = append(*slice, value)
//	return nil
//}

// ----------
// --hook option
const poshHook = `function scoop { if ($args[0] -eq "search") { scoop-search-multisource.exe @($args | Select-Object -Skip 1) } else { scoop.ps1 @args } }`
