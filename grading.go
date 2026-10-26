package main

import "strings"

// Discogs grades items by the Goldmine Standard. Gifts from Discogs may only
// be bought in Mint or Near Mint, for both the media and the sleeve. The
// English texts are Discogs' own ("How To Grade Items", support.discogs.com,
// July 2026); where Discogs writes one text for the whole item, it is used for
// both parts. The Russian texts are translations of them.

const (
	formatVinyl    = "vinyl"
	formatCD       = "cd"
	formatCassette = "cassette"
)

// grading is the four definitions shown for one format.
type grading struct {
	MediaMint, MediaNM, SleeveMint, SleeveNM string
}

var gradings = map[string]map[string]grading{
	"en": {
		formatVinyl: {
			MediaMint:  "Absolutely perfect in every way. Certainly never been played, possibly even still sealed. Should be used sparingly as a grade, if at all.",
			MediaNM:    "A nearly perfect record. A NM or M- record has more than likely never been played, and the vinyl will play perfectly, with no imperfections during playback. Many dealers won't give a grade higher than this implying (perhaps correctly) that no record is ever truly perfect. The record should show no obvious signs of wear.",
			SleeveMint: "Absolutely perfect in every way. Certainly never been played, possibly even still sealed. Should be used sparingly as a grade, if at all.",
			SleeveNM:   "A 45 RPM or EP sleeve should have no more than the most minor defects, such as any sign of slight handling. An LP cover should have no creases, folds, seam splits, cut-out holes, or other noticeable similar defects. The same should be true of any other inserts, such as posters, lyric sleeves, etc.",
		},
		formatCD: {
			MediaMint:  "Perfect. No scuffs/scratches, unplayed - possibly still sealed.",
			MediaNM:    "Near perfect. No obvious signs of use, it may have been played - but it has been handled very carefully.",
			SleeveMint: "Insert/Inlay/Booklet/Sleeve/Digipak: Perfect. No wear, marks, or any other imperfections - possibly still sealed.",
			SleeveNM:   "Insert/Inlay/Booklet/Sleeve/Digipak: Near Perfect. No obvious wear, it may have only the slightest of marks from handling.",
		},
		formatCassette: {
			MediaMint:  "Cassette is brand new, and professionally produced. Used sparingly as a grade, should be free of even the slightest blemishes and/or defects. This grade should be used sparingly, if at all.",
			MediaNM:    "Tape is likely new, free of any wear or damage.",
			SleeveMint: "J-Card is crisp, clean and perfect in every way. Likely sealed.",
			SleeveNM:   "Sleeve should be totally crisp and clean with only the slightest evidence of handling.",
		},
	},
	"ru": {
		formatVinyl: {
			MediaMint:  "Абсолютно идеальное состояние во всех отношениях. Точно ни разу не проигрывалась, возможно, даже ещё запечатана. Эту оценку следует ставить редко, если вообще ставить.",
			MediaNM:    "Почти идеальная пластинка. Пластинку NM или M- скорее всего ни разу не проигрывали, и она играет идеально, без каких-либо дефектов при воспроизведении. Многие продавцы не ставят оценку выше этой, подразумевая (возможно, справедливо), что идеальных пластинок не бывает. На пластинке не должно быть явных следов износа.",
			SleeveMint: "Абсолютно идеальное состояние во всех отношениях. Точно ни разу не проигрывалась, возможно, даже ещё запечатана. Эту оценку следует ставить редко, если вообще ставить.",
			SleeveNM:   "На конверте сингла (45 об/мин) или EP допускаются лишь самые незначительные дефекты, например лёгкие следы того, что его брали в руки. На конверте LP не должно быть заломов, сгибов, разошедшихся швов, вырубок (cut-out) и других подобных заметных дефектов. То же относится ко всем вкладкам: постерам, внутренним конвертам с текстами и т. п.",
		},
		formatCD: {
			MediaMint:  "Идеальный. Ни потёртостей, ни царапин, не проигрывался — возможно, ещё запечатан.",
			MediaNM:    "Почти идеальный. Никаких явных следов использования; его могли проигрывать, но обращались с ним очень бережно.",
			SleeveMint: "Вкладка, буклет, обложка, дигипак: идеальные. Никакого износа, пометок или других недостатков — возможно, ещё запечатаны.",
			SleeveNM:   "Вкладка, буклет, обложка, дигипак: почти идеальные. Никакого явного износа, допустимы лишь едва заметные следы того, что их брали в руки.",
		},
		formatCassette: {
			MediaMint:  "Кассета совершенно новая, заводского производства. Не должно быть даже малейших изъянов или дефектов. Эту оценку следует ставить редко, если вообще ставить.",
			MediaNM:    "Плёнка, скорее всего, новая, без износа и повреждений.",
			SleeveMint: "Вкладыш (J-card) свежий, чистый и идеальный во всех отношениях. Скорее всего, запечатана.",
			SleeveNM:   "Вкладыш совершенно свежий и чистый, лишь с едва заметными следами того, что его брали в руки.",
		},
	},
}

// gradingFor returns the definitions for a format; unknown formats get the
// vinyl ones.
func gradingFor(lang, format string) grading {
	byFormat := gradings[lang]
	if byFormat == nil {
		byFormat = gradings["en"]
	}
	if g, ok := byFormat[format]; ok {
		return g
	}
	return byFormat[formatVinyl]
}

// formatFromNames maps Discogs format names (release "formats[].name") to
// vinyl, cd or cassette; the first recognised one wins.
func formatFromNames(names []string) string {
	for _, n := range names {
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "vinyl":
			return formatVinyl
		case "cd", "cdr", "sacd":
			return formatCD
		case "cassette":
			return formatCassette
		}
	}
	return ""
}

// formatFromDescription maps a listing's "2xLP, Album, RE, Vio" style text.
func formatFromDescription(desc string) string {
	for _, part := range strings.Split(desc, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if i := strings.Index(p, "x"); i > 0 && strings.Trim(p[:i], "0123456789") == "" {
			p = p[i+1:] // "2xlp" → "lp", "3x12\"" → "12\""
		}
		switch {
		case p == "lp" || p == "vinyl" || p == "7\"" || p == "10\"" || p == "12\"":
			return formatVinyl
		case p == "cd" || p == "cdr" || p == "sacd":
			return formatCD
		case strings.HasPrefix(p, "cass"):
			return formatCassette
		}
	}
	return ""
}
