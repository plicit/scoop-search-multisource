package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	//	"github.com/pkg/profile"
)

var version = "v0.1.20220410"

type SearchQuery struct {
	pattern *regexp.Regexp
	fields  []string
}

type SourceRef struct {
	cond string
	kind string
	path string
}

type SourceRefs []SourceRef

type AppInfo struct {
	name        string
	version     string
	description string
	homepage    string
	bins        []string
}

type AppList = []*AppInfo
type BucketMap = map[string]AppList
type NameSourceMap = map[string]string

type BucketsMatch struct {
	buckets BucketMap
	numApps int
}

func NewBucketsMatch() *BucketsMatch {
	return &BucketsMatch{make(BucketMap), 0}
}

type SearchState struct {
	args               *parsedArgs
	query              *SearchQuery
	sources            SourceRefs
	numSourcesSearched int

	// Each Source has a corresponding BucketsMatch here:
	matchList     []*BucketsMatch
	numAppMatches int
}

var g_State = new(SearchState)

// load and search (filter) the given source, returning filtered bucket matches and the total number of app matches in those buckets
func (state *SearchState) SearchSource(src *SourceRef) (match *BucketsMatch, err error) {
	// load buckets based upon type of source
	buckets, err := loadBucketsFrom(src.kind, src.path)
	if err != nil {
		return nil, fmt.Errorf("unable to get buckets from source: %s", src)
	}

	match = filterBuckets(state.query, buckets)
	match.buckets = renameBucketsToKnownNames(match.buckets, "/")

	fmt.Printf(colorize("source.summary", "- %d apps matched in %d/%d buckets\n\n"), match.numApps, len(match.buckets), len(buckets))

	state.matchList = append(state.matchList, match)
	state.numAppMatches += match.numApps

	return match, nil
}

func (state *SearchState) Run(args *parsedArgs) error {
	state.args = args
	state.query = &args.query
	state.sources = args.sources
	// g_State.index = 0
	// g_State.numAppMatches = 0
	// g_State.matches

	merge := args.merge

	divider := colorize("divider", "____________________\n")

	if args.debug {
		fmt.Printf(colorize("debug", "  QUERY")+": %s\n", state.query.pattern)
		fmt.Printf(colorize("debug", " FIELDS")+": %s\n", strings.Join(state.query.fields, ","))
		fmt.Printf(colorize("debug", "SOURCES")+": %v\n", state.sources)
		fmt.Printf(colorize("debug", " COLORS")+": %v\n", state.args.colors.StringAll(true))
	}

	for index, src := range state.sources {
		if src.cond == "if0" && state.numAppMatches > 0 {
			continue
		}

		fmt.Print(divider)
		fmt.Printf(colorize("source.header", "#%d Searching %s [%s] %s\n"), index+1, src.cond, src.kind, src.path)

		match, _ := state.SearchSource(&src)
		state.numSourcesSearched += 1
		if !merge {
			printResults(match.buckets, args.linelen)
		}
	}

	// calculate the total number of buckets matching from each Source
	numBuckets := 0
	for _, match := range state.matchList {
		numBuckets += len(match.buckets)
	}

	fmt.Print(divider)
	fmt.Printf(colorize("totals", "TOTAL: %d apps matched in %d buckets from %d sources\n\n"), state.numAppMatches, numBuckets, state.numSourcesSearched)

	if merge {
		fmt.Printf("MERGED RESULTS:\n\n")
		merged := NewBucketsMatch()
		for _, match := range state.matchList {
			for name, appList := range match.buckets {
				merged.buckets[name] = appList
				merged.numApps += len(appList)
			}
		}
		printResults(merged.buckets, args.linelen)
	}

	return nil
}

// resolves the path to scoop folder
func getScoopHome() (res string) {
	if value, ok := os.LookupEnv("SCOOP"); ok {
		res = value
	} else {
		var err error
		res, err = os.UserHomeDir()
		checkWith(err, "Could not determine home dir")
		res += "\\scoop"
	}
	return
}

func timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf("%s took %s\n", name, elapsed)
}

func main() {
	// uncomment to profile:
	//defer profile.Start().Stop() // CPU profiling by default

	defer timeTrack(time.Now(), "Search")

	initColorize()

	args := parseArgs()
	mergeColorMap(args.colors)

	// don't allow an empty query
	// if the user wants all apps, they can supply a dot or empty string "" which will have "(?i)" prepended to it and skip this
	if args.query.pattern.String() == "" {
		myUsage()
	} else {
		// merge semantic color names into color values

		g_State.Run(args)
	}

	// exit with status code
	if g_State.numAppMatches == 0 {
		os.Exit(1)
	}
}

// Search by filtering apps and buckets
func filterApp(query *SearchQuery, app *AppInfo) bool {
	found := false
	//	if strings.Contains(strings.ToLower(app.name), opt) {
	for _, field := range query.fields {
		switch field {
		case "name":
			if query.pattern.MatchString(app.name) {
				app.bins = nil // ignore bin if name matches
				found = true
			}
		case "bins":
			var bins []string
			for _, bin := range app.bins {
				bin = filepath.Base(bin)
				//			if strings.Contains(strings.ToLower(strings.TrimSuffix(bin, filepath.Ext(bin))), opt) {
				if query.pattern.MatchString(strings.TrimSuffix(bin, filepath.Ext(bin))) {
					bins = append(bins, bin)
					found = true
				}
			}
			app.bins = bins
		case "description":
			if query.pattern.MatchString(app.description) {
				found = true
			}
		}
	}
	return found
}

func filterAppList(query *SearchQuery, apps AppList) (matches AppList) {
	matches = AppList{}
	for _, app := range apps {
		if filterApp(query, app) {
			matches = append(matches, app)
		}
	}

	// sort the apps by name
	sort.SliceStable(matches, func(i, j int) bool {
		// case insensitive comparison where hyphens are ignored
		return strings.ToLower(strings.ReplaceAll(matches[i].name, "-", "")) <= strings.ToLower(strings.ReplaceAll(matches[j].name, "-", ""))
	})

	return
}

// uses query to filter buckets into a new BucketMap of matches
func filterBuckets(query *SearchQuery, buckets BucketMap) (match *BucketsMatch) {
	match = NewBucketsMatch()
	match.numApps = 0
	match.buckets = BucketMap{}
	for source, apps := range buckets {
		apps = filterAppList(query, apps)
		if len(apps) > 0 {
			match.buckets[source] = apps
			match.numApps += len(apps)
		}
	}
	return
}

// uses %SCOOP%\apps\scoop\current\buckets.json to rename bucket source paths/urls to known names
func renameBucketsToKnownNames(buckets BucketMap, prefix string) (res BucketMap) {
	// we will remove any local buckets path prefix
	bucketsPath := getScoopHome() + "\\buckets\\"

	// load known bucket name:source from buckets.json
	bucketsJsonFile := filepath.Join(getScoopHome(), "apps", "scoop", "current", "buckets.json")
	bucketsByName := loadNameSourceMapFromJsonFile(bucketsJsonFile)
	bucketsBySource := map[string]string{}

	// create a reverse map that is source:name
	for name, source := range bucketsByName {
		bucketsBySource[source] = name
	}

	// create a bucket map that uses the known names for known sources
	res = BucketMap{}
	for source, applist := range buckets {
		if name, ok := bucketsBySource[source]; ok {
			res[prefix+name] = applist
		} else {
			// if source was a local bucket in $SCOOP/buckets, then trim the directory
			trimmed := strings.TrimPrefix(source, bucketsPath)
			if trimmed != source {
				source = prefix + trimmed
			}
			res[source] = applist
		}
	}

	return res
}

// print the given buckets as search results
func printResults(buckets BucketMap, linelen int) (anyMatches bool) {
	// sort by bucket names
	//fmt.Printf("colors=%v\n", g_State.args.colors)
	entries := 0
	sortedKeys := make([]string, 0, len(buckets))
	for k := range buckets {
		entries += len(buckets[k])
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	installed := loadInstalledApps(getScoopHome() + "\\apps")

	// reserve additional space assuming each variable string has length 1. Will save time on initial allocations
	var display strings.Builder
	display.Grow((len(sortedKeys)*12 + entries*11))

	for _, k := range sortedKeys {
		v := buckets[k]

		if len(v) > 0 {
			anyMatches = true

			line := ""
			line = fmt.Sprintf(colorize("bucket.header", "'%s' bucket:\n"), k)
			display.WriteString(line)

			for _, m := range v {
				prefix := "    "
				color := "app.name"
				if _, exists := installed[m.name]; exists {
					color = "app.name.installed"
					prefix = " ** "
				}

				line = colorize(color, prefix+m.name)
				line += " (" + colorize("app.version", m.version) + ")"

				if len(m.bins) != 0 {
					// display.WriteString(" --> includes '")
					// bins := strings.Join(m.bins, ",")
					bins := m.bins[0]
					line += " [" + bins + "]"
				}
				display.WriteString(line)

				remainder := MaxInt(0, linelen-len(line)-2)
				if len(m.description) != 0 && remainder > 0 {
					display.WriteString(": ")
					display.WriteString(colorize("app.description", m.description[:MinInt(remainder, len(m.description))]))
				}
				display.WriteString("\n")
			}
			display.WriteString("\n")
		}
	}

	if !anyMatches {
		display.WriteString("No matches found.\n")
	}

	os.Stdout.WriteString(display.String())
	return
}
