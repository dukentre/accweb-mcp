package app

import (
	"sort"
	"strings"
)

type accTrack struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Country string   `json:"country"`
	Aliases []string `json:"aliases,omitempty"`
}

func accTracksPayload() map[string]any {
	return map[string]any{"tracks": accTracks()}
}

func accTracks() []accTrack {
	return []accTrack{
		{ID: "barcelona", Name: "Circuit de Barcelona-Catalunya", Country: "Spain", Aliases: []string{"barcelona", "catalunya", "барселона", "каталунья"}},
		{ID: "brands_hatch", Name: "Brands Hatch", Country: "United Kingdom", Aliases: []string{"brands hatch", "брендс хэтч", "брэндс хэтч"}},
		{ID: "hungaroring", Name: "Hungaroring", Country: "Hungary", Aliases: []string{"hungary", "хунгароринг", "венгрия"}},
		{ID: "misano", Name: "Misano World Circuit Marco Simoncelli", Country: "Italy", Aliases: []string{"misano", "мизано"}},
		{ID: "monza", Name: "Monza", Country: "Italy", Aliases: []string{"монца", "монза"}},
		{ID: "nurburgring", Name: "Nurburgring", Country: "Germany", Aliases: []string{"nürburgring", "nurburgring gp", "нюрбургринг"}},
		{ID: "paul_ricard", Name: "Circuit Paul Ricard", Country: "France", Aliases: []string{"paul ricard", "поль рикар", "поль-рикар"}},
		{ID: "silverstone", Name: "Silverstone", Country: "United Kingdom", Aliases: []string{"silverstone", "сильверстоун"}},
		{ID: "spa", Name: "Spa-Francorchamps", Country: "Belgium", Aliases: []string{"spa francorchamps", "spa-francorchamps", "спа", "спа франкоршам"}},
		{ID: "zandvoort", Name: "Zandvoort", Country: "Netherlands", Aliases: []string{"zandvoort", "зандворт", "зандфорт"}},
		{ID: "zolder", Name: "Zolder", Country: "Belgium", Aliases: []string{"zolder", "золдер"}},
		{ID: "kyalami", Name: "Kyalami Grand Prix Circuit", Country: "South Africa", Aliases: []string{"kyalami", "кьялами"}},
		{ID: "laguna_seca", Name: "WeatherTech Raceway Laguna Seca", Country: "United States", Aliases: []string{"laguna seca", "лагуна сека"}},
		{ID: "mount_panorama", Name: "Mount Panorama Circuit", Country: "Australia", Aliases: []string{"bathurst", "mount panorama", "батерст", "маунт панорама"}},
		{ID: "suzuka", Name: "Suzuka Circuit", Country: "Japan", Aliases: []string{"suzuka", "сузука"}},
		{ID: "imola", Name: "Imola", Country: "Italy", Aliases: []string{"imola", "autodromo imola", "имола"}},
		{ID: "donington", Name: "Donington Park", Country: "United Kingdom", Aliases: []string{"donington park", "donington", "донингтон", "доннингтон"}},
		{ID: "oulton_park", Name: "Oulton Park", Country: "United Kingdom", Aliases: []string{"oulton park", "оултон парк"}},
		{ID: "snetterton", Name: "Snetterton Circuit", Country: "United Kingdom", Aliases: []string{"snetterton", "снеттертон"}},
		{ID: "cota", Name: "Circuit of the Americas", Country: "United States", Aliases: []string{"circuit of the americas", "austin", "cota", "кота", "остин"}},
		{ID: "indianapolis", Name: "Indianapolis Motor Speedway", Country: "United States", Aliases: []string{"indy", "indianapolis", "индианаполис"}},
		{ID: "watkins_glen", Name: "Watkins Glen International", Country: "United States", Aliases: []string{"watkins glen", "уоткинс глен"}},
		{ID: "valencia", Name: "Circuit Ricardo Tormo", Country: "Spain", Aliases: []string{"valencia", "ricardo tormo", "валенсия"}},
		{ID: "red_bull_ring", Name: "Red Bull Ring", Country: "Austria", Aliases: []string{"red bull ring", "ред булл ринг", "шпильберг", "spielberg"}},
		{ID: "nurburgring_24h", Name: "24H Nurburgring", Country: "Germany", Aliases: []string{"nürburgring 24h", "nurburgring 24h", "nordschleife", "нюрбургринг 24", "нордшляйфе"}},
	}
}

func findACCTrack(id string) (accTrack, bool) {
	needle := normalizeTrackCompletionValue(id)
	for _, track := range accTracks() {
		if normalizeTrackCompletionValue(track.ID) == needle {
			return track, true
		}
	}
	return accTrack{}, false
}

func completeACCTrackIDs(value string) []string {
	value = normalizeTrackCompletionValue(value)
	matches := make([]trackCompletionMatch, 0, len(accTracks()))
	for _, track := range accTracks() {
		if score, ok := trackCompletionScore(track, value); ok {
			matches = append(matches, trackCompletionMatch{id: track.ID, score: score})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return matches[i].id < matches[j].id
	})

	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.id)
		if len(out) == 100 {
			break
		}
	}
	return out
}

type trackCompletionMatch struct {
	id    string
	score int
}

func trackCompletionScore(track accTrack, value string) (int, bool) {
	if value == "" {
		return 10, true
	}

	best := 100
	best = min(best, completionCandidateScore(track.ID, value, 0, 1, 5))
	best = min(best, completionCandidateScore(track.Name, value, 2, 3, 6))
	for _, alias := range track.Aliases {
		best = min(best, completionCandidateScore(alias, value, 2, 4, 6))
	}
	if best == 100 {
		return 0, false
	}
	return best, true
}

func completionCandidateScore(candidate, value string, exactScore, prefixScore, containsScore int) int {
	candidate = normalizeTrackCompletionValue(candidate)
	switch {
	case candidate == value:
		return exactScore
	case strings.HasPrefix(candidate, value):
		return prefixScore
	case len([]rune(value)) >= 3 && strings.Contains(candidate, value):
		return containsScore
	default:
		return 100
	}
}

func normalizeTrackCompletionValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	return strings.Join(strings.Fields(value), " ")
}
