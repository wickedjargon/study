package quiz

// British and American spellings are folded together before any other
// expansion: every token is rewritten to its American form, on both sides of
// a comparison, so "5 metres" and "5 meters" agree — and unit expansions
// only need to emit one spelling. An explicit word list, never suffix
// rules: a blanket -our→-or would mangle four, hour, your, and tour.
var britishSpellings = map[string]string{
	// -re / -er, with plurals.
	"metre": "meter", "metres": "meters",
	"kilometre": "kilometer", "kilometres": "kilometers",
	"centimetre": "centimeter", "centimetres": "centimeters",
	"millimetre": "millimeter", "millimetres": "millimeters",
	"litre": "liter", "litres": "liters",
	"millilitre": "milliliter", "millilitres": "milliliters",
	"centre": "center", "centres": "centers",
	"theatre": "theater", "theatres": "theaters",
	"fibre": "fiber", "fibres": "fibers",

	// -our / -or.
	"colour": "color", "colours": "colors",
	"favourite": "favorite", "favourites": "favorites",
	"honour": "honor", "honours": "honors",
	"neighbour": "neighbor", "neighbours": "neighbors",
	"flavour": "flavor", "flavours": "flavors",
	"behaviour": "behavior", "behaviours": "behaviors",
	"labour": "labor", "labours": "labors",
	"harbour": "harbor", "harbours": "harbors",
	"armour": "armor", "humour": "humor",
	"rumour": "rumor", "rumours": "rumors",
	"vapour": "vapor", "odour": "odor",

	// -ise / -ize verb families (explicit inflections).
	"realise": "realize", "realises": "realizes", "realised": "realized", "realising": "realizing",
	"recognise": "recognize", "recognises": "recognizes", "recognised": "recognized", "recognising": "recognizing",
	"organise": "organize", "organises": "organizes", "organised": "organized", "organising": "organizing",
	"apologise": "apologize", "apologises": "apologizes", "apologised": "apologized", "apologising": "apologizing",
	"criticise": "criticize", "criticises": "criticizes", "criticised": "criticized", "criticising": "criticizing",
	"emphasise": "emphasize", "emphasises": "emphasizes", "emphasised": "emphasized", "emphasising": "emphasizing",
	"memorise": "memorize", "memorises": "memorizes", "memorised": "memorized", "memorising": "memorizing",
	"summarise": "summarize", "summarises": "summarizes", "summarised": "summarized", "summarising": "summarizing",
	"organisation": "organization", "organisations": "organizations",
	"realisation": "realization", "realisations": "realizations",

	// Assorted.
	"grey": "gray", "greys": "grays",
	"practise": "practice", "practises": "practices", "practised": "practiced", "practising": "practicing",
	"defence": "defense", "defences": "defenses",
	"offence": "offense", "offences": "offenses",
	"licence": "license", "licences": "licenses",
	"travelling": "traveling", "travelled": "traveled", "traveller": "traveler", "travellers": "travelers",
	"cancelled": "canceled", "cancelling": "canceling",
	"jewellery": "jewelry",
	"catalogue": "catalog", "catalogues": "catalogs",
	"dialogue": "dialog", "dialogues": "dialogs",
	"analogue": "analog", "analogues": "analogs",
	"programme": "program", "programmes": "programs",
	"cheque": "check", "cheques": "checks",
	"aluminium": "aluminum",
	"moustache": "mustache", "moustaches": "mustaches",
	"doughnut": "donut", "doughnuts": "donuts",
	"plough": "plow", "ploughs": "plows",
	"tyre": "tire", "tyres": "tires",
	"kerb": "curb", "kerbs": "curbs",
	"pyjamas": "pajamas",
	"whisky": "whiskey",
	"storey": "story", "storeys": "stories",
	"draught": "draft", "draughts": "drafts",
	"mum": "mom", "mums": "moms",
}

// canonicalSpelling rewrites one token to its American form, if it has one.
func canonicalSpelling(tok string) string {
	if us, ok := britishSpellings[tok]; ok {
		return us
	}
	return tok
}
