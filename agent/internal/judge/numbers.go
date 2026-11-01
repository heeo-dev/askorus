package judge

// Numbers (part 5, 숫자). Code reads the quantities of a unit's text so it
// can tell when two numbers of one kind cannot be the same value. That only
// raises a hand (numberMismatch, numbersAbsent, Item.NumbersDiffer and the
// bold NumberSpans): code cannot tell whether two numbers measure the same
// thing, so it never changes a judge's label.
//
// Every value is an interval, everything a reader would accept for it:
//   - A plain number (no hedge word) is exact: half a unit of its last
//     digit either way, and its first two significant digits at least
//     (numExactPlace, rnum-7). "8분 19초" is 8:18.5–8:19.5, "3.75%"
//     3.745–3.755%, "123,456" 123,455.5–123,456.5. From 10,000 up its last
//     digit is the place of its last nonzero digit or a tenth of the place
//     of its first digit, whichever is smaller: "20,000원" is 19,500–
//     20,500원, so it does not fit "15,000원", and "1억" is 0.95억–1.05억.
//     Below 10,000 it is the ones at least, as every zero is a digit there.
//     The spelling never changes it: "2만 원" reads as "20,000원", "$50M"
//     as "$50 million" (49.5M–50.5M), "2천" and "2 thousand" as "2,000".
//   - A hedged one (약, 정도, 쯤, 가량, 안팎, 내외, about, around, ~, …)
//     for a whole unit of its last digit: "약 8분 3초" is 8:02–8:04, "약
//     956만" 955만–957만. A round hedged number of 10,000 or more counts its
//     last nonzero digit as its last digit (numRoundPlace): "930만" is
//     rounded to 10만, so "약 930만" is 920만–940만 and holds 9,279,861, and
//     "약 20,000원" is 10,000–30,000원. Below 10,000 a zero is usually a
//     real digit ("약 8분 10초", "약 30%", "약 200mg"). The line is what
//     lets both P0-6 pairs come out right: "약 930만" fits "9,279,861명",
//     and "약 8분 10초" (8:09–8:11) does not fit "약 8분 3초" (8:02–8:04).
//     Read as rounded to 10 seconds, as 930만 is to 10만, the 10초 would
//     span 8:00–8:20 and hold 8:03. Here too "$50M" reads as "$50 million"
//     ("about $50M" is 40M–60M).
//   - "N여" and "N 남짓" are N and some more, up to the next step of N's
//     last nonzero digit: "100여 명" is 100–200, "20년 남짓" 20–30 years.
//   - A range spans from its first end to its last, each end with its own
//     width as a plain or a hedged number, and a multiplier written once is
//     both ends' ("3~4만 원" is 2.95만–4.05만). A limit's value has the
//     width of a number too ("20,000원 이상" is not met by "19,000원").
//     A date spans its day, month or year, "1700년대" and "the 1800s" a
//     century; "thousands" 1,000–10,000.
//
// Two values agree (Compatible) when they have the same kind and class and
// their intervals overlap; ends that only touch do not count, so "200 mg"
// and "300 mg" differ. Limits are different (C2-8): two one-sided limits
// agree only when they point the same way at the same value, a limit given
// as a range (200~300mg 이하) only with the same range, and a value and a
// one-sided limit when the value as written satisfies the limit. So
// "200~300mg/일 이하" does not fit "200mg 미만", although [0, 300] and
// [0, 200) overlap.
//
// Classes keep apart what one kind holds: a measure's dimension (length,
// mass, …), a currency, a counter (명, 잔, …; a count without one, as in
// "약 930만", fits any), and % against %p. An amount a day reads as the
// amount ("200mg/일" and "하루 200mg" are said both ways).
//
// Reading is one pass with bounded look-back and look-ahead, in time
// proportional to the text. What it cannot read gives no quantity: bare
// small numbers (model names like "iPhone 18", ports) are left out, a list
// marker at the start of a line ("1. 서울", "3) 인구 10만 명") is no number,
// and neither are digits inside words (A20, CO2, HTTP/2, COVID-19), links,
// inline math, times of day (오후 2시 30분), ordinals after 제 (제3조),
// numbers joined by a hyphen (1544-0010) or a times sign (1920×1080).
// English hedge and limit words match in any case ("About", "No more
// than"), and the longer of two wins ("no more than" is no "more than").

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// Shapes of a value.
const (
	numPoint      uint8 = iota // a value or a range: everything in [lo, hi]
	numLimit                   // one side of a value: 이상, 이하, at least, under, …
	numRangeLimit              // a limit given as a range: "200~300mg 이하"
)

// numValue is the value behind a Quantity, in the base unit of its class
// (seconds, metres, grams, litres, °C, …; days for a date, months for a
// year).
type numValue struct {
	class string // what is counted inside the kind; "" for a count without a counter
	shape uint8  // numPoint, numLimit or numRangeLimit
	upper bool   // a limit from above (이하, 미만, at most, under); else from below
	// [lo, loHi] is the first end and [hiLo, hi] the last end; a single
	// value has both ends the same. A limit holds its limit value.
	lo, loHi, hiLo, hi float64
	nomLo, nomHi       float64 // the ends as written
	ver                []int   // NumVersion: its parts
	// NumDate: yearless means no year was written and lo… are days of the
	// year. A date with a year keeps its days of the year in dayLo, dayHi
	// too, to be compared with one without (0, 366 when it spans years).
	yearless     bool
	dayLo, dayHi float64
}

// numUnit is a word that tells what a number counts.
type numUnit struct {
	word   string
	kind   string
	class  string
	scale  float64 // to the base unit of the class
	offset float64 // added before scale (°F)
}

// Classes of measures and percentages. Durations, dates, years, ratios
// and versions have one class each.
const (
	numLength = "length" // metres
	numMass   = "mass"   // grams
	numVolume = "volume" // litres
	numTemp   = "temp"   // °C
	numDegree = "degree" // ° or 도: an angle, or a temperature in Korean
	numSpeed  = "speed"  // metres a second
	numData   = "data"   // bytes
	numEnergy = "energy" // joules
	numPower  = "power"  // watts
	numArea   = "area"   // square metres
	numPress  = "press"  // mmHg
	numFreq   = "freq"   // hertz
	numSound  = "sound"  // dB
	numPct    = "%"      // percent
	numPctPt  = "%p"     // percentage points
)

// Durations in seconds.
const (
	numMin   = 60.0
	numHour  = 3600.0
	numDay   = 86400.0
	numWeek  = 7 * numDay
	numYear  = 365.25 * numDay
	numMonth = numYear / 12
)

// numUnits are the words after a number. Words of ASCII letters need a
// non-letter after them; Korean words need a space, punctuation, the end or
// a particle (numKoTails).
var numUnits = []numUnit{
	// Durations.
	{"초", NumDuration, "", 1, 0}, {"분", NumDuration, "", numMin, 0},
	{"시간", NumDuration, "", numHour, 0}, {"일", NumDuration, "", numDay, 0},
	{"주일", NumDuration, "", numWeek, 0}, {"주", NumDuration, "", numWeek, 0},
	{"개월", NumDuration, "", numMonth, 0}, {"달", NumDuration, "", numMonth, 0},
	{"년", NumDuration, "", numYear, 0},
	{"seconds", NumDuration, "", 1, 0}, {"second", NumDuration, "", 1, 0},
	{"secs", NumDuration, "", 1, 0}, {"sec", NumDuration, "", 1, 0},
	{"minutes", NumDuration, "", numMin, 0}, {"minute", NumDuration, "", numMin, 0},
	{"mins", NumDuration, "", numMin, 0}, {"min", NumDuration, "", numMin, 0},
	{"hours", NumDuration, "", numHour, 0}, {"hour", NumDuration, "", numHour, 0},
	{"hrs", NumDuration, "", numHour, 0}, {"hr", NumDuration, "", numHour, 0},
	{"h", NumDuration, "", numHour, 0},
	{"days", NumDuration, "", numDay, 0}, {"day", NumDuration, "", numDay, 0},
	{"weeks", NumDuration, "", numWeek, 0}, {"week", NumDuration, "", numWeek, 0},
	{"months", NumDuration, "", numMonth, 0}, {"month", NumDuration, "", numMonth, 0},
	{"years", NumDuration, "", numYear, 0}, {"year", NumDuration, "", numYear, 0},
	{"yrs", NumDuration, "", numYear, 0}, {"yr", NumDuration, "", numYear, 0},
	{"decades", NumDuration, "", 10 * numYear, 0}, {"decade", NumDuration, "", 10 * numYear, 0},

	// Percent and percentage points.
	{"%포인트", NumPercent, numPctPt, 1, 0}, {"%p", NumPercent, numPctPt, 1, 0}, {"%P", NumPercent, numPctPt, 1, 0},
	{"%", NumPercent, numPct, 1, 0}, {"％", NumPercent, numPct, 1, 0},
	{"퍼센트포인트", NumPercent, numPctPt, 1, 0}, {"퍼센트", NumPercent, numPct, 1, 0},
	{"percentage points", NumPercent, numPctPt, 1, 0}, {"percentage point", NumPercent, numPctPt, 1, 0},
	{"pp", NumPercent, numPctPt, 1, 0},
	{"percent", NumPercent, numPct, 1, 0}, {"per cent", NumPercent, numPct, 1, 0}, {"pct", NumPercent, numPct, 1, 0},

	// Money.
	{"원", NumMoney, "KRW", 1, 0}, {"won", NumMoney, "KRW", 1, 0}, {"KRW", NumMoney, "KRW", 1, 0},
	{"달러", NumMoney, "USD", 1, 0}, {"dollars", NumMoney, "USD", 1, 0}, {"dollar", NumMoney, "USD", 1, 0},
	{"USD", NumMoney, "USD", 1, 0}, {"센트", NumMoney, "USD", 0.01, 0},
	{"cents", NumMoney, "USD", 0.01, 0}, {"cent", NumMoney, "USD", 0.01, 0},
	{"유로", NumMoney, "EUR", 1, 0}, {"euros", NumMoney, "EUR", 1, 0}, {"euro", NumMoney, "EUR", 1, 0},
	{"EUR", NumMoney, "EUR", 1, 0},
	{"엔", NumMoney, "JPY", 1, 0}, {"yen", NumMoney, "JPY", 1, 0}, {"JPY", NumMoney, "JPY", 1, 0},
	{"위안", NumMoney, "CNY", 1, 0}, {"yuan", NumMoney, "CNY", 1, 0}, {"CNY", NumMoney, "CNY", 1, 0},
	{"파운드", NumMoney, "GBP", 1, 0}, {"GBP", NumMoney, "GBP", 1, 0},

	// Counts, by counter; English nouns take the Korean counter.
	{"명", NumCount, "명", 1, 0}, {"인분", NumCount, "인분", 1, 0}, {"인", NumCount, "명", 1, 0},
	{"개국", NumCount, "개국", 1, 0}, {"개사", NumCount, "개사", 1, 0}, {"개", NumCount, "개", 1, 0},
	{"잔", NumCount, "잔", 1, 0}, {"곳", NumCount, "곳", 1, 0}, {"회", NumCount, "회", 1, 0},
	{"번", NumCount, "번", 1, 0}, {"건", NumCount, "건", 1, 0}, {"마리", NumCount, "마리", 1, 0},
	{"권", NumCount, "권", 1, 0}, {"세대", NumCount, "세대", 1, 0}, {"가구", NumCount, "세대", 1, 0},
	{"가지", NumCount, "가지", 1, 0}, {"종", NumCount, "종", 1, 0}, {"표", NumCount, "표", 1, 0},
	{"층", NumCount, "층", 1, 0}, {"위", NumCount, "위", 1, 0}, {"샷", NumCount, "샷", 1, 0},
	{"단계", NumCount, "단계", 1, 0}, {"세", NumCount, "세", 1, 0}, {"쪽", NumCount, "쪽", 1, 0},
	{"페이지", NumCount, "쪽", 1, 0}, {"병", NumCount, "병", 1, 0}, {"알", NumCount, "알", 1, 0},
	{"people", NumCount, "명", 1, 0}, {"persons", NumCount, "명", 1, 0}, {"person", NumCount, "명", 1, 0},
	{"residents", NumCount, "명", 1, 0}, {"employees", NumCount, "명", 1, 0}, {"workers", NumCount, "명", 1, 0},
	{"participants", NumCount, "명", 1, 0}, {"patients", NumCount, "명", 1, 0}, {"students", NumCount, "명", 1, 0},
	{"adults", NumCount, "명", 1, 0}, {"children", NumCount, "명", 1, 0}, {"users", NumCount, "명", 1, 0},
	{"deaths", NumCount, "명", 1, 0},
	{"cups", NumCount, "잔", 1, 0}, {"cup", NumCount, "잔", 1, 0},
	{"items", NumCount, "개", 1, 0}, {"countries", NumCount, "개국", 1, 0}, {"cases", NumCount, "건", 1, 0},
	{"floors", NumCount, "층", 1, 0}, {"stories", NumCount, "층", 1, 0}, {"storeys", NumCount, "층", 1, 0},
	{"nights", NumCount, "night", 1, 0}, {"night", NumCount, "night", 1, 0},
	{"votes", NumCount, "표", 1, 0}, {"households", NumCount, "세대", 1, 0}, {"species", NumCount, "종", 1, 0},
	{"steps", NumCount, "단계", 1, 0}, {"pages", NumCount, "쪽", 1, 0}, {"servings", NumCount, "인분", 1, 0},
	{"shots", NumCount, "샷", 1, 0},

	// Ratios. "times" is how often when "a" or "per" follows
	// (numTimesCount).
	{"배", NumRatio, "", 1, 0}, {"times", NumRatio, "", 1, 0}, {"×", NumRatio, "", 1, 0},
	{"x", NumRatio, "", 1, 0}, {"-fold", NumRatio, "", 1, 0},

	// Length, and the measures written with a length unit inside.
	{"nm", NumMeasure, numLength, 1e-9, 0}, {"nanometers", NumMeasure, numLength, 1e-9, 0},
	{"nanometres", NumMeasure, numLength, 1e-9, 0}, {"nanometer", NumMeasure, numLength, 1e-9, 0},
	{"나노미터", NumMeasure, numLength, 1e-9, 0},
	{"µm", NumMeasure, numLength, 1e-6, 0}, {"μm", NumMeasure, numLength, 1e-6, 0},
	{"micrometers", NumMeasure, numLength, 1e-6, 0}, {"micrometres", NumMeasure, numLength, 1e-6, 0},
	{"micrometer", NumMeasure, numLength, 1e-6, 0}, {"microns", NumMeasure, numLength, 1e-6, 0},
	{"micron", NumMeasure, numLength, 1e-6, 0}, {"마이크로미터", NumMeasure, numLength, 1e-6, 0},
	{"마이크론", NumMeasure, numLength, 1e-6, 0},
	{"mm", NumMeasure, numLength, 1e-3, 0}, {"millimeters", NumMeasure, numLength, 1e-3, 0},
	{"millimetres", NumMeasure, numLength, 1e-3, 0}, {"millimeter", NumMeasure, numLength, 1e-3, 0},
	{"밀리미터", NumMeasure, numLength, 1e-3, 0},
	{"cm", NumMeasure, numLength, 1e-2, 0}, {"centimeters", NumMeasure, numLength, 1e-2, 0},
	{"centimetres", NumMeasure, numLength, 1e-2, 0}, {"centimeter", NumMeasure, numLength, 1e-2, 0},
	{"센티미터", NumMeasure, numLength, 1e-2, 0}, {"센티", NumMeasure, numLength, 1e-2, 0},
	{"km/h", NumMeasure, numSpeed, 1 / 3.6, 0}, {"km/s", NumMeasure, numSpeed, 1000, 0},
	{"kph", NumMeasure, numSpeed, 1 / 3.6, 0}, {"m/s", NumMeasure, numSpeed, 1, 0},
	{"mph", NumMeasure, numSpeed, 0.44704, 0},
	{"km²", NumMeasure, numArea, 1e6, 0}, {"m²", NumMeasure, numArea, 1, 0},
	{"제곱미터", NumMeasure, numArea, 1, 0}, {"평", NumMeasure, numArea, 3.305785, 0},
	{"ha", NumMeasure, numArea, 1e4, 0}, {"헥타르", NumMeasure, numArea, 1e4, 0},
	{"acres", NumMeasure, numArea, 4046.86, 0},
	{"km", NumMeasure, numLength, 1e3, 0}, {"kilometers", NumMeasure, numLength, 1e3, 0},
	{"kilometres", NumMeasure, numLength, 1e3, 0}, {"kilometer", NumMeasure, numLength, 1e3, 0},
	{"kilometre", NumMeasure, numLength, 1e3, 0}, {"킬로미터", NumMeasure, numLength, 1e3, 0},
	{"mmHg", NumMeasure, numPress, 1, 0},
	{"m", NumMeasure, numLength, 1, 0}, {"meters", NumMeasure, numLength, 1, 0},
	{"metres", NumMeasure, numLength, 1, 0}, {"meter", NumMeasure, numLength, 1, 0},
	{"metre", NumMeasure, numLength, 1, 0}, {"미터", NumMeasure, numLength, 1, 0},
	{"AU", NumMeasure, numLength, 1.495978707e11, 0},
	{"light-years", NumMeasure, numLength, 9.4607e15, 0}, {"light years", NumMeasure, numLength, 9.4607e15, 0},
	{"광년", NumMeasure, numLength, 9.4607e15, 0},
	{"inches", NumMeasure, numLength, 0.0254, 0}, {"inch", NumMeasure, numLength, 0.0254, 0},
	{"인치", NumMeasure, numLength, 0.0254, 0},
	{"feet", NumMeasure, numLength, 0.3048, 0}, {"foot", NumMeasure, numLength, 0.3048, 0},
	{"ft", NumMeasure, numLength, 0.3048, 0}, {"피트", NumMeasure, numLength, 0.3048, 0},
	{"miles", NumMeasure, numLength, 1609.344, 0}, {"mile", NumMeasure, numLength, 1609.344, 0},
	{"마일", NumMeasure, numLength, 1609.344, 0},
	{"yards", NumMeasure, numLength, 0.9144, 0}, {"야드", NumMeasure, numLength, 0.9144, 0},

	// Mass.
	{"mg", NumMeasure, numMass, 1e-3, 0}, {"milligrams", NumMeasure, numMass, 1e-3, 0},
	{"milligram", NumMeasure, numMass, 1e-3, 0}, {"밀리그램", NumMeasure, numMass, 1e-3, 0},
	{"µg", NumMeasure, numMass, 1e-6, 0}, {"μg", NumMeasure, numMass, 1e-6, 0},
	{"mcg", NumMeasure, numMass, 1e-6, 0}, {"micrograms", NumMeasure, numMass, 1e-6, 0},
	{"kg", NumMeasure, numMass, 1e3, 0}, {"kilograms", NumMeasure, numMass, 1e3, 0},
	{"kilogram", NumMeasure, numMass, 1e3, 0}, {"킬로그램", NumMeasure, numMass, 1e3, 0},
	{"g", NumMeasure, numMass, 1, 0}, {"grams", NumMeasure, numMass, 1, 0},
	{"gram", NumMeasure, numMass, 1, 0}, {"그램", NumMeasure, numMass, 1, 0},
	{"tonnes", NumMeasure, numMass, 1e6, 0}, {"tonne", NumMeasure, numMass, 1e6, 0},
	{"tons", NumMeasure, numMass, 1e6, 0}, {"ton", NumMeasure, numMass, 1e6, 0},
	{"톤", NumMeasure, numMass, 1e6, 0},
	{"lbs", NumMeasure, numMass, 453.592, 0}, {"lb", NumMeasure, numMass, 453.592, 0},
	{"oz", NumMeasure, numMass, 28.3495, 0}, {"ounces", NumMeasure, numMass, 28.3495, 0},

	// Volume.
	{"mL", NumMeasure, numVolume, 1e-3, 0}, {"ml", NumMeasure, numVolume, 1e-3, 0},
	{"milliliters", NumMeasure, numVolume, 1e-3, 0}, {"millilitres", NumMeasure, numVolume, 1e-3, 0},
	{"밀리리터", NumMeasure, numVolume, 1e-3, 0}, {"cc", NumMeasure, numVolume, 1e-3, 0},
	{"dL", NumMeasure, numVolume, 0.1, 0}, {"dl", NumMeasure, numVolume, 0.1, 0},
	{"L", NumMeasure, numVolume, 1, 0}, {"liters", NumMeasure, numVolume, 1, 0},
	{"litres", NumMeasure, numVolume, 1, 0}, {"liter", NumMeasure, numVolume, 1, 0},
	{"litre", NumMeasure, numVolume, 1, 0}, {"리터", NumMeasure, numVolume, 1, 0},
	{"gallons", NumMeasure, numVolume, 3.78541, 0}, {"gallon", NumMeasure, numVolume, 3.78541, 0},
	{"갤런", NumMeasure, numVolume, 3.78541, 0},

	// Temperature and degrees.
	{"°C", NumMeasure, numTemp, 1, 0}, {"℃", NumMeasure, numTemp, 1, 0},
	{"°F", NumMeasure, numTemp, 5.0 / 9, -32}, {"℉", NumMeasure, numTemp, 5.0 / 9, -32},
	{"°", NumMeasure, numDegree, 1, 0}, {"도", NumMeasure, numDegree, 1, 0},
	{"degrees", NumMeasure, numDegree, 1, 0},

	// Data, energy, power, frequency, sound.
	{"KB", NumMeasure, numData, 1e3, 0}, {"kB", NumMeasure, numData, 1e3, 0},
	{"MB", NumMeasure, numData, 1e6, 0}, {"GB", NumMeasure, numData, 1e9, 0},
	{"TB", NumMeasure, numData, 1e12, 0}, {"PB", NumMeasure, numData, 1e15, 0},
	{"bytes", NumMeasure, numData, 1, 0}, {"kilobytes", NumMeasure, numData, 1e3, 0},
	{"megabytes", NumMeasure, numData, 1e6, 0}, {"gigabytes", NumMeasure, numData, 1e9, 0},
	{"terabytes", NumMeasure, numData, 1e12, 0},
	{"메가바이트", NumMeasure, numData, 1e6, 0}, {"기가바이트", NumMeasure, numData, 1e9, 0},
	{"테라바이트", NumMeasure, numData, 1e12, 0}, {"기가", NumMeasure, numData, 1e9, 0},
	{"테라", NumMeasure, numData, 1e12, 0},
	{"kWh", NumMeasure, numEnergy, 3.6e6, 0}, {"MWh", NumMeasure, numEnergy, 3.6e9, 0},
	{"GWh", NumMeasure, numEnergy, 3.6e12, 0}, {"Wh", NumMeasure, numEnergy, 3600, 0},
	{"kcal", NumMeasure, numEnergy, 4184, 0}, {"킬로칼로리", NumMeasure, numEnergy, 4184, 0},
	{"칼로리", NumMeasure, numEnergy, 4184, 0}, {"calories", NumMeasure, numEnergy, 4184, 0},
	{"calorie", NumMeasure, numEnergy, 4184, 0}, {"kJ", NumMeasure, numEnergy, 1e3, 0},
	{"MJ", NumMeasure, numEnergy, 1e6, 0}, {"J", NumMeasure, numEnergy, 1, 0},
	{"kW", NumMeasure, numPower, 1e3, 0}, {"MW", NumMeasure, numPower, 1e6, 0},
	{"GW", NumMeasure, numPower, 1e9, 0}, {"W", NumMeasure, numPower, 1, 0},
	{"watts", NumMeasure, numPower, 1, 0}, {"kilowatts", NumMeasure, numPower, 1e3, 0},
	{"킬로와트", NumMeasure, numPower, 1e3, 0}, {"와트", NumMeasure, numPower, 1, 0},
	{"GHz", NumMeasure, numFreq, 1e9, 0}, {"MHz", NumMeasure, numFreq, 1e6, 0},
	{"kHz", NumMeasure, numFreq, 1e3, 0}, {"Hz", NumMeasure, numFreq, 1, 0},
	{"헤르츠", NumMeasure, numFreq, 1, 0},
	{"dB", NumMeasure, numSound, 1, 0}, {"데시벨", NumMeasure, numSound, 1, 0},

	// One-glyph units of the CJK compatibility block, as Korean text often
	// writes them ("605㎢", "2㎏"), at the scales of the units in letters
	// (S6R-1: without them "605㎢" and "700㎢" had no numbers to clash).
	{"㎚", NumMeasure, numLength, 1e-9, 0}, {"㎛", NumMeasure, numLength, 1e-6, 0},
	{"㎜", NumMeasure, numLength, 1e-3, 0}, {"㎝", NumMeasure, numLength, 1e-2, 0},
	{"㎞", NumMeasure, numLength, 1e3, 0},
	{"㎟", NumMeasure, numArea, 1e-6, 0}, {"㎠", NumMeasure, numArea, 1e-4, 0},
	{"㎡", NumMeasure, numArea, 1, 0}, {"㎢", NumMeasure, numArea, 1e6, 0},
	{"㏊", NumMeasure, numArea, 1e4, 0},
	{"㎕", NumMeasure, numVolume, 1e-6, 0}, {"㎖", NumMeasure, numVolume, 1e-3, 0},
	{"㎗", NumMeasure, numVolume, 0.1, 0}, {"㎘", NumMeasure, numVolume, 1e3, 0},
	{"㏄", NumMeasure, numVolume, 1e-3, 0}, {"㎣", NumMeasure, numVolume, 1e-6, 0},
	{"㎤", NumMeasure, numVolume, 1e-3, 0}, {"㎥", NumMeasure, numVolume, 1e3, 0},
	{"㎦", NumMeasure, numVolume, 1e12, 0},
	{"㎍", NumMeasure, numMass, 1e-6, 0}, {"㎎", NumMeasure, numMass, 1e-3, 0},
	{"㎏", NumMeasure, numMass, 1e3, 0},
	{"㎧", NumMeasure, numSpeed, 1, 0},
	{"㎅", NumMeasure, numData, 1e3, 0}, {"㎆", NumMeasure, numData, 1e6, 0},
	{"㎇", NumMeasure, numData, 1e9, 0},
	{"㎉", NumMeasure, numEnergy, 4184, 0},
	{"㎾", NumMeasure, numPower, 1e3, 0}, {"㎿", NumMeasure, numPower, 1e6, 0},
	{"㎐", NumMeasure, numFreq, 1, 0}, {"㎑", NumMeasure, numFreq, 1e3, 0},
	{"㎒", NumMeasure, numFreq, 1e6, 0}, {"㎓", NumMeasure, numFreq, 1e9, 0},
	{"㏈", NumMeasure, numSound, 1, 0},
}

// numUnitIndex lists numUnits by first byte, longest word first, so the
// longest unit wins ("개월" before "개", "%p" before "%", "km/h" before
// "km").
var numUnitIndex = func() (idx [256][]*numUnit) {
	for i := range numUnits {
		u := &numUnits[i]
		idx[u.word[0]] = append(idx[u.word[0]], u)
	}
	for b := range idx {
		l := idx[b]
		for i := 1; i < len(l); i++ {
			for j := i; j > 0 && len(l[j].word) > len(l[j-1].word); j-- {
				l[j], l[j-1] = l[j-1], l[j]
			}
		}
	}
	return idx
}()

// Units made on the way: a count of times, the fraction of "3분의 1", and a
// count written without a counter ("약 930만"), which fits any counter.
var (
	numTimesCount = numUnit{word: "times", kind: NumCount, class: "회", scale: 1}
	numFraction   = numUnit{word: "분의", kind: NumRatio, scale: 1}
	numBareCount  = numUnit{kind: NumCount, scale: 1}
)

// numKoTails may follow a Korean unit or counter: particles and endings
// that attach to a number. Any other syllable makes the unit part of
// another word (원칙, 분기, 분석), so it is no unit.
var numKoTails = []string{
	"입니다", "이었", "였", "이며", "이고", "이다", "이라", "이란", "이나", "이요",
	"이상", "이하", "이내", "미만", "초과", "정도", "가량", "안팎", "내외", "남짓", "전후", "가까이", "무렵",
	"동안", "까지", "부터", "보다", "처럼", "마다", "짜리", "밖에", "넘게", "넘는", "넘어", "넘을", "넘은",
	"으로", "에서", "에는", "에도",
	"이", "가", "을", "를", "은", "는", "의", "에", "로", "와", "과", "도", "만", "씩", "째", "간", "쯤",
	"전", "후", "뒤", "내", "안", "당", "대", "경", "께", "여", "며", "고", "다", "라", "란", "나", "인", "요", "임", "치", "급",
	"면", "지", "죠",
}

// Words before a number: hedges and limits, nearest first. Words need a
// boundary before them: no letter for English ones, no Hangul syllable for
// Korean ones ("요약 3개" has no 약). A tilde hedges only when it touches
// the number ("~4%"); with a space it is more often a range sign.
var (
	numTildes       = []string{"~", "∼"}
	numHedgesBefore = []string{
		"approximately", "approx.", "approx", "roughly", "around", "about", "nearly", "almost", "circa", "ca.",
		"어림잡아", "대략", "얼추", "거의", "약", "≈",
	}
	numLowerBefore = []string{
		"no less than", "no fewer than", "not less than", "not fewer than", "at least", "more than", "greater than",
		"upwards of", "exceeding", "above", "over", "최소한", "최소", "적어도", "≥", ">=", ">", "＞",
	}
	numUpperBefore = []string{
		"no more than", "not more than", "no greater than", "not greater than", "not exceeding", "at most", "up to",
		"less than", "fewer than", "within", "below", "under", "최대", "많아야", "≤", "<=", "<", "＜",
	}
	numBetween     = []string{"between"}
	numSpeedBefore = []struct {
		word string
		per  float64
	}{{"초속", 1}, {"분속", numMin}, {"시속", numHour}}
)

// Words after a number and its unit: hedges and limits.
var (
	numHedgesAfter = []string{"정도", "가량", "안팎", "내외", "전후", "가까이", "무렵", "쯤", "or so", "-ish", "ish"}
	numOverAfter   = []string{"남짓"} // the value and some more, as N여
	numLowerAfter  = []string{
		"이상", "초과", "넘게", "넘는", "넘어", "넘을", "넘은",
		"or more", "or higher", "or above", "or greater", "or longer", "or older", "and up", "and above",
	}
	numUpperAfter = []string{
		"이하", "미만", "이내", "or less", "or fewer", "or lower", "or below", "or shorter", "or under",
	}
)

// Currencies written before a number.
var numCurrencies = []*numUnit{
	{word: "US$", kind: NumMoney, class: "USD", scale: 1}, {word: "$", kind: NumMoney, class: "USD", scale: 1},
	{word: "€", kind: NumMoney, class: "EUR", scale: 1}, {word: "£", kind: NumMoney, class: "GBP", scale: 1},
	{word: "¥", kind: NumMoney, class: "JPY", scale: 1}, {word: "₩", kind: NumMoney, class: "KRW", scale: 1},
	{word: "USD ", kind: NumMoney, class: "USD", scale: 1}, {word: "KRW ", kind: NumMoney, class: "KRW", scale: 1},
	{word: "EUR ", kind: NumMoney, class: "EUR", scale: 1},
}

// Range signs between two values. A hyphen counts only between digits with
// no space ("3-5"; dates like 2026-10-16 are read before); "to" only with
// spaces; "and" only after "between". An em dash is punctuation.
var numRangeSeps = []string{"~", "∼", "〜", "～", "–", "-", "to"}

// Month names, for English dates.
var numMonths = []struct {
	word string
	m    int
}{
	{"January", 1}, {"February", 2}, {"March", 3}, {"April", 4}, {"May", 5}, {"June", 6},
	{"July", 7}, {"August", 8}, {"September", 9}, {"October", 10}, {"November", 11}, {"December", 12},
	{"Sept", 9}, {"Jan", 1}, {"Feb", 2}, {"Mar", 3}, {"Apr", 4}, {"Jun", 6}, {"Jul", 7}, {"Aug", 8},
	{"Sep", 9}, {"Oct", 10}, {"Nov", 11}, {"Dec", 12},
}

// Numbers written as words, each spanning its order of magnitude:
// "thousands of times", "수천 배", "수만~수십만 년". Longest first.
var (
	numVagueEN = []struct {
		word   string
		lo, hi float64
	}{
		{"hundreds of thousands", 1e5, 1e6}, {"tens of thousands", 1e4, 1e5},
		{"hundreds of millions", 1e8, 1e9}, {"tens of millions", 1e7, 1e8},
		{"thousands", 1e3, 1e4}, {"hundreds", 1e2, 1e3}, {"millions", 1e6, 1e7},
		{"billions", 1e9, 1e10}, {"dozens", 24, 100},
	}
	numVagueKO = []struct {
		word   string
		lo, hi float64
	}{
		{"수십만", 2e5, 1e6}, {"수백만", 2e6, 1e7}, {"수천만", 2e7, 1e8}, {"수십억", 2e9, 1e10},
		{"수백억", 2e10, 1e11}, {"수천억", 2e11, 1e12},
		{"수십", 20, 100}, {"수백", 200, 1000}, {"수천", 2000, 1e4}, {"수만", 2e4, 1e5},
		{"수억", 2e8, 1e9}, {"수조", 2e12, 1e13},
	}
)

// numRead is one value as read, before ranges, hedges and limits apply.
type numRead struct {
	start, end int
	unit       *numUnit // nil when none was written
	v, place   float64  // in the unit as written; place is that of its last digit as a hedge reads it (numRoundPlace)
	vague      bool     // v..vHi is a span of its own: "thousands", "수천"
	vHi        float64
	date       *numDate // a date or a year
	ver        []int    // a version
	keep       bool     // bare, but written with separators or a multiplier: a count
	year4      bool     // bare whole number of four digits, 1000–2999: a year if nothing else
	plus       bool     // "20+": at least
	over       bool     // "10여", "100명 남짓", "2%대": the value and some more, up to its next step
	mult       float64  // the multiplier written after its first digits (만, 천만, million, k); 0 for none
	big        float64  // read as Korean (numKo): the 만, 억 or 조 of its first group, 1 for none; else 0
	nz         float64  // the place of its last nonzero digit, as written
}

// numDate is a date or a year as read.
type numDate struct {
	kind     string // NumDate or NumYear
	y, m     int    // the year (0 when none) and month
	d1, d2   int    // NumDate: the days of the month it spans
	m1, m2   int    // NumYear: the months it spans, 0 = January (winter runs to 13)
	decade   bool   // NumYear: 1990년대, the 1990s (a century for 1700년대, the 1800s)
	yearless bool
}

// numByteAt is s[j], or 0 past the end.
func numByteAt(s string, j int) byte {
	if j < len(s) {
		return s[j]
	}
	return 0
}

func numIsDigit(c byte) bool  { return c >= '0' && c <= '9' }
func numIsLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }
func numIsHangul(r rune) bool { return r >= 0xAC00 && r <= 0xD7A3 }

// Numbers finds the quantities in a unit's text, in order: digits with
// separators and decimals, Korean 천·만·억·조 and their composites
// ("1억 4,960만"), ranges ("200~300", "10 to 100 times"), limits (이상,
// 이하, 미만, 초과, at least, under, …) and hedges (약, 안팎, 쯤, about, …),
// each with its kind. A plain value is exact: half its last digit either
// way, to its first two significant digits at least ("20,000원" and "2만
// 원" are 19,500–20,500원); a hedged one allows a whole last digit, where a
// round number of 10,000 or more counts its trailing zeros as rounding
// ("약 20,000원" is 10,000–30,000원). It reads text once and never
// fails: what it cannot read gives no quantity. Inline math ($…$, \(…\),
// as InlineMath finds it) is skipped: the phone draws it with KaTeX, and
// its "$" is no dollar. So is a time of day ("오후 2시 30분").
func Numbers(text string) []Quantity {
	sc := numScan{s: text, math: InlineMath(text)}
	if len(sc.math) > 0 {
		// Math becomes spaces, so no look-ahead reads into it; offsets stay.
		masked := []byte(text)
		for _, m := range sc.math {
			for k := m.Start; k < m.End; k++ {
				masked[k] = ' '
			}
		}
		sc.s = string(masked)
	}
	sc.run()
	for i := range sc.out {
		sc.out[i].Text = text[sc.out[i].Start:sc.out[i].End]
	}
	return sc.out
}

// numScan reads one text.
type numScan struct {
	s    string
	out  []Quantity
	last int        // end of the last quantity or math span: look-backs stop here
	math []MathSpan // the inline math, which s has as spaces
	// rangeCur is the currency written before a range's first end while
	// its last end is read, so that end takes a K, M or B ("$5–10M").
	rangeCur *numUnit
}

func (sc *numScan) run() {
	s := sc.s
	for i, m := 0, 0; i < len(s); {
		if m < len(sc.math) && i >= sc.math[m].Start {
			i, sc.last = max(i, sc.math[m].End), max(sc.last, sc.math[m].End)
			m++
			continue
		}
		c := s[i]
		switch {
		case numIsDigit(c):
			if end := sc.listMarker(i); end > i {
				i, sc.last = end, end
				continue
			}
			if sc.digitStart(i) {
				if end := numClock(s, i); end > i {
					i, sc.last = end, end
					continue
				}
				if end, ok := sc.quantity(i); ok {
					i = end
					continue
				}
			}
			i = numSkipToken(s, i)
			continue
		case numIsLetter(c):
			if i > 0 && (numIsLetter(s[i-1]) || numIsDigit(s[i-1])) {
				i = numSkipToken(s, i)
				continue
			}
			if end := numLinkEnd(s, i); end > i {
				i = end
				continue
			}
			if end, ok := sc.quantity(i); ok { // "thousands of", "v2.1"
				i = end
				continue
			}
			i = numSkipToken(s, i)
			continue
		case c == 0xEC && strings.HasPrefix(s[i:], "수"):
			if end, ok := sc.quantity(i); ok { // "수천 배"
				i = end
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
}

// listMarker is the end of a list marker at i, or i when there is none:
// at the start of the text or of a line, after spaces, one to three digits
// with "." or ")" and a space after them are an item's number, no quantity
// ("1. 서울", "3) 인구 10만 명", "12. 마지막"). The rest of the line is read
// as any text: "1. 2배" has 2배. "3.5배" and "2026. 10. 30." start with no
// marker.
func (sc *numScan) listMarker(i int) int {
	s := sc.s
	k := i
	for k > sc.last && (s[k-1] == ' ' || s[k-1] == '\t') {
		k--
	}
	if k > 0 && (k <= sc.last || s[k-1] != '\n') {
		return i // not at the start of a line (or after inline math)
	}
	j := i
	for j < len(s) && numIsDigit(s[j]) && j-i < 4 {
		j++
	}
	if n := j - i; n < 1 || n > 3 || j+1 >= len(s) || s[j] != '.' && s[j] != ')' || s[j+1] != ' ' && s[j+1] != '\t' {
		return i
	}
	return j + 1
}

// numClock is the end of a time of day at i, "2시", "2시 30분" or "14시
// 5분 3초", or i when there is none: a time of day is no quantity, and its
// minutes are no duration. "2시간" is a duration and "3시즌" no time.
func numClock(s string, i int) int {
	h, j, ok := numSmall(s, i, 2)
	if !ok || h > 24 || !strings.HasPrefix(s[j:], "시") || strings.HasPrefix(s[j:], "시간") || numKoWordAfter(s, j+len("시")) {
		return i
	}
	end := j + len("시")
	for _, unit := range []string{"분", "초"} {
		k := end
		if k < len(s) && s[k] == ' ' {
			k++
		}
		n, k2, ok := numSmall(s, k, 2)
		if !ok || n > 59 || !strings.HasPrefix(s[k2:], unit) || numKoWordAfter(s, k2+len(unit)) {
			break
		}
		end = k2 + len(unit)
	}
	return end
}

// numSkipToken skips letters and digits from i, and at least one byte.
func numSkipToken(s string, i int) int {
	j := i + 1
	for j < len(s) && (numIsLetter(s[j]) || numIsDigit(s[j])) {
		j++
	}
	return j
}

// numLinkEnd is the end of a link that starts at i (http://, https://,
// www.), or i when none does: digits in links are no quantities.
func numLinkEnd(s string, i int) int {
	if !strings.HasPrefix(s[i:], "http://") && !strings.HasPrefix(s[i:], "https://") && !strings.HasPrefix(s[i:], "www.") {
		return i
	}
	j := i
	for j < len(s) && strings.IndexByte(" \t\n)]>", s[j]) < 0 {
		j++
	}
	return j
}

// digitStart reports whether a number may start at the digit at i: not
// inside a number or a word (A20, x86, COVID-19, HTTP/2, CO2, #3), not as
// a power (10^8) or the second number of 1920×1080, and not as an ordinal
// after 제 (제3조, 제2항, 제5회: an article or an event, no amount).
func (sc *numScan) digitStart(i int) bool {
	s := sc.s
	if i == 0 {
		return true
	}
	p := s[i-1]
	switch {
	case numIsDigit(p), numIsLetter(p), p == '_', p == '#', p == '@', p == '^':
		return false
	case p == '.' || p == ',' || p == '-' || p == '/':
		return i < 2 || !(numIsLetter(s[i-2]) || numIsDigit(s[i-2]))
	case i >= len("제") && s[i-len("제"):i] == "제":
		r, _ := utf8.DecodeLastRuneInString(s[:i-len("제")])
		return numIsHangul(r) // 제 inside a word ("문제3"), not one of its own
	}
	return !numAfterTimes(s, i)
}

// numCore is a written number: digits, separators and a decimal part.
type numCore struct {
	end    int
	val    float64
	place  float64 // of its last digit
	nz     float64 // of its last nonzero digit (= place with a decimal part)
	whole  bool    // no decimal part
	groups bool    // thousands separators
	n      int     // digits before any decimal point
	comps  []int   // three or more dotted parts: 1.99.1, 2026.10.16
}

// numDigits reads the digits at i. A run of more than 15 digits is no
// number.
func numDigits(s string, i int) (numCore, bool) {
	c := numCore{whole: true, place: 1, nz: 1}
	j := i
	for j < len(s) && numIsDigit(s[j]) {
		c.val = c.val*10 + float64(s[j]-'0')
		j++
	}
	c.n = j - i
	if c.n == 0 || c.n > 15 {
		return c, false
	}
	if c.n <= 3 {
		for j+4 <= len(s) && s[j] == ',' && numIsDigit(s[j+1]) && numIsDigit(s[j+2]) && numIsDigit(s[j+3]) &&
			(j+4 == len(s) || !numIsDigit(s[j+4])) && c.n+3 <= 15 {
			c.val = c.val*1000 + float64(s[j+1]-'0')*100 + float64(s[j+2]-'0')*10 + float64(s[j+3]-'0')
			c.n += 3
			c.groups = true
			j += 4
		}
	}
	if c.val > 0 {
		for k := j - 1; k >= i && (s[k] == '0' || s[k] == ','); k-- {
			if s[k] == '0' {
				c.nz *= 10
			}
		}
	}
	if j+1 < len(s) && s[j] == '.' && numIsDigit(s[j+1]) {
		k := j + 1
		var frac float64
		part := 0
		for k < len(s) && numIsDigit(s[k]) && k-j <= 9 {
			frac = frac*10 + float64(s[k]-'0')
			part = part*10 + int(s[k]-'0')
			k++
		}
		if k < len(s) && numIsDigit(s[k]) {
			return c, false
		}
		// Three or more dotted parts are a version or a date.
		if k+1 < len(s) && s[k] == '.' && numIsDigit(s[k+1]) && !c.groups && c.val < 1e9 {
			comps := []int{int(c.val), part}
			for k+1 < len(s) && s[k] == '.' && numIsDigit(s[k+1]) && len(comps) < 8 {
				n, m := 0, k+1
				for m < len(s) && numIsDigit(s[m]) && m-k <= 9 {
					n = n*10 + int(s[m]-'0')
					m++
				}
				if m < len(s) && numIsDigit(s[m]) {
					return c, false
				}
				comps = append(comps, n)
				k = m
			}
			c.comps, c.end = comps, k
			return c, true
		}
		c.whole = false
		c.place = math.Pow(10, -float64(k-j-1))
		c.nz = c.place
		c.val += frac * c.place
		j = k
	}
	c.end = j
	return c, true
}

// numKoGroup is a Korean number read on from its first digits: one group
// below 만 and the big unit after it ("3천500만", "5백50만", "930만", "3천").
type numKoGroup struct {
	v            float64 // its value
	place, nz    float64 // the places of its last written digit and its last nonzero one
	end          int
	big          float64 // 만, 억 or 조; 1 for none
	mult         float64 // what multiplies its first digits: 1e4 for "4만", 1e7 for "4천만"
	thousandOnly bool    // only 천 after the first digits: "5천"
}

// numKo reads what goes on after the digits of c: 천 or 백 with the digits
// after them ("3천500", "3천5백", "3천5백50", "5백50"), then 만, 억 or 조,
// which multiplies the whole group ("3천500만" is 3,500만, not 3천 and
// 500만). ok is false when no 천, 백 or big unit follows. 백 counts only
// with digits or a big unit after it: alone it is more often another
// word. A decimal ("1.5억", "1.5천만") takes 천 only before a big unit.
func numKo(s string, c numCore) (g numKoGroup, ok bool) {
	g = numKoGroup{v: c.val, place: c.place, nz: c.nz, end: c.end, big: 1, mult: 1}
	// lower adds the digits d of the place below: "3천" and then "500".
	lower := func(d numCore, scale float64, end int) {
		g.v += d.val * scale
		g.place, g.end = d.place*scale, end
		if d.val > 0 {
			g.nz = d.nz * scale
		}
	}
	small := false
	switch {
	case strings.HasPrefix(s[g.end:], "천") && (c.whole || numBigAt(s, g.end+len("천")) > 1):
		small, g.thousandOnly = true, true
		g.v, g.place, g.nz, g.mult, g.end = g.v*1e3, g.place*1e3, g.nz*1e3, 1e3, g.end+len("천")
		if !c.whole {
			break
		}
		if d, ok := numKoDigits(s, g.end, 10); ok && strings.HasPrefix(s[d.end:], "백") {
			lower(d, 100, d.end+len("백"))
			if k, ok := numKoDigits(s, g.end, 100); ok {
				lower(k, 1, k.end)
			}
			g.thousandOnly = false
		} else if d, ok := numKoDigits(s, g.end, 1000); ok {
			lower(d, 1, d.end)
			g.thousandOnly = false
		}
	case c.whole && strings.HasPrefix(s[g.end:], "백"):
		e := g.end + len("백")
		k, digits := numKoDigits(s, e, 100)
		if !digits && numBigAt(s, e) == 1 {
			break
		}
		small = true
		g.v, g.place, g.nz, g.mult, g.end = g.v*100, g.place*100, g.nz*100, 100, e
		if digits {
			lower(k, 1, k.end)
		}
	}
	if big := numBigAt(s, g.end); big > 1 {
		g.v, g.place, g.nz, g.mult, g.big = g.v*big, g.place*big, g.nz*big, g.mult*big, big
		g.end += len("만") // 만, 억 and 조 are three bytes each
		g.thousandOnly = false
		return g, true
	}
	return g, small
}

// numBigAt is the big unit at j: 1e4 for 만, 1e8 for 억, 1e12 for 조, or 1.
func numBigAt(s string, j int) float64 {
	switch {
	case strings.HasPrefix(s[j:], "만"):
		return 1e4
	case strings.HasPrefix(s[j:], "억"):
		return 1e8
	case strings.HasPrefix(s[j:], "조"):
		return 1e12
	}
	return 1
}

// numKoDigits reads whole digits at j, right after 천 or 백, below limit.
func numKoDigits(s string, j int, limit float64) (numCore, bool) {
	d, ok := numDigits(s, j)
	return d, ok && d.whole && !d.groups && d.comps == nil && d.val < limit
}

// numMultEN reads an English multiplier after a number at j: " million",
// " thousand", "k", "bn".
func numMultEN(s string, j int) (float64, int) {
	if j < len(s) && s[j] == 'k' && (j+1 == len(s) || !numIsLetter(s[j+1])) {
		return 1e3, 1
	}
	if strings.HasPrefix(s[j:], "bn") && (j+2 == len(s) || !numIsLetter(s[j+2])) {
		return 1e9, 2
	}
	k := j
	if k < len(s) && s[k] == ' ' {
		k++
	}
	for _, w := range []struct {
		word string
		mult float64
	}{{"thousand", 1e3}, {"million", 1e6}, {"billion", 1e9}, {"trillion", 1e12}} {
		if strings.HasPrefix(s[k:], w.word) && (k+len(w.word) == len(s) || !numIsLetter(s[k+len(w.word)])) {
			return w.mult, k + len(w.word) - j
		}
	}
	return 0, 0
}

// numBare reports whether r is a bare small number, no quantity.
func numBare(r numRead) bool {
	return r.unit == nil && r.date == nil && r.ver == nil && !r.keep && !r.year4 && !r.vague
}

// quantity reads the quantity that starts at i (a digit, or a word that
// begins a number: "thousands", "수천", "v2") and adds it.
func (sc *numScan) quantity(i int) (int, bool) {
	s := sc.s
	a, ok := sc.value(i, sc.last)
	if !ok {
		return 0, false
	}
	b, isRange, end := a, false, a.end
	between := numWordBefore(s, numBackSpace(s, a.start, sc.last), sc.last, numBetween) > 0
	// A range: "200~300mg", "10 to 100 times", "between 10 and 20".
	if j, sign, ok := numRangeSep(s, a.end, between, a.date != nil); ok {
		if slices.Contains(numCurrencies, a.unit) {
			sc.rangeCur = a.unit
		}
		b2, ok := sc.value(j, j)
		sc.rangeCur = nil
		if ok && (sign == 0 || b2.date == nil && b2.ver == nil && !b2.vague) {
			if sign > 0 {
				b2.v, b2.start, b2.year4 = -b2.v, j-sign, false
			}
			a2 := a
			numShareMult(&a2, &b2)
			if numSameScale(&a2, &b2) && numNominal(a2) <= numNominal(b2) && (!strings.HasPrefix(s[a.end:], "부터") || b2.date != nil) {
				a, b, isRange, end = a2, b2, true, b2.end
				// "October 27–28, 2026": the year after the range is both ends'.
				if a.date != nil && a.date.yearless && b.date.yearless {
					if y, e, ok := numYearAfter(s, end); ok {
						da, db := *a.date, *b.date
						da.y, da.yearless, db.y, db.yearless = y, false, y, false
						a.date, b.date, end = &da, &db, e
					}
				}
			}
		}
	}
	// 1544-0010, 1588-1234: a phone or model number, not a year or a range.
	if !isRange && a.unit == nil && a.date == nil && a.ver == nil && numJoined(s, a.end) {
		return 0, false
	}
	if numBare(a) && numBare(b) {
		return 0, false
	}
	start, hedged, lower, upper := sc.before(a.start)
	// 초속 30만 km is a speed, 섭씨 30도 a temperature.
	if u := a.unit; u != nil && b.unit == u {
		k := numBackSpace(s, start, sc.last)
		switch {
		case u.class == numLength:
			for _, p := range numSpeedBefore {
				if n := numWordBefore(s, k, sc.last, []string{p.word}); n > 0 {
					a.unit = &numUnit{word: p.word, kind: NumMeasure, class: numSpeed, scale: u.scale / p.per}
					b.unit, start = a.unit, k-n
					break
				}
			}
		case u.class == numDegree:
			if n := numWordBefore(s, k, sc.last, []string{"섭씨"}); n > 0 {
				a.unit = &numUnit{word: "섭씨", kind: NumMeasure, class: numTemp, scale: 1}
				b.unit, start = a.unit, k-n
			} else if n := numWordBefore(s, k, sc.last, []string{"화씨"}); n > 0 {
				a.unit = &numUnit{word: "화씨", kind: NumMeasure, class: numTemp, scale: 5.0 / 9, offset: -32}
				b.unit, start = a.unit, k-n
			}
		}
	}
	// "200만 원대", "2%대": the value and up to its next step, as 여 is
	// ("50년대" is a decade, no duration).
	over := false
	if u := b.unit; u != nil && u.word != "년" && b.date == nil && strings.HasPrefix(s[end:], "대") && !numKoWordAfter(s, end+len("대")) {
		over, end = true, end+len("대")
	}
	// Hedges and limits after it: "970만 명 안팎", "200mg 미만", "or more";
	// 남짓 is the last end and some more, as 여 is.
	end, h2, l2, u2, o2 := numAfter(s, end)
	o2 = o2 || over
	hedged = hedged || h2
	lower = lower || l2 || a.plus || b.plus
	upper = upper || u2
	if o2 {
		b.over = true
		a.over = a.over || !isRange
	}
	q, ok := numBuild(s, start, end, a, b, isRange, hedged, lower, upper)
	if !ok {
		return 0, false
	}
	sc.out = append(sc.out, q)
	sc.last = end
	return end, true
}

// numNominal is the value of r as written, in its base unit, to order the
// ends of a range.
func numNominal(r numRead) float64 {
	switch {
	case r.date != nil:
		lo, _ := numDateSpan(r.date)
		return lo
	case r.unit != nil:
		return (r.v + r.unit.offset) * r.unit.scale
	}
	return r.v
}

// numShareMult gives the first end of a range the multiplier written once,
// after the last end: "3~4만 원" is 3만~4만 원, "3,000~4,000만" 3,000만~
// 4,000만 and "5–10 million" 5–10 million, as in Korean and English the
// multiplier is both ends'. A first end with a multiplier of its own takes
// none, but one with only its own 천 or 백 takes the last end's 만, 억 or
// 조 ("3천~5천만" is 3천만~5천만, "3천~5,000만" 3천만~5,000만). A first end
// with a unit takes none, but a currency sign is no such unit ("$5–10M" is
// $5M–$10M, "$1.5–2 billion" $1.5–2 billion). Only when the range stays
// in order ("5,000~2만" stays 5,000 to 20,000).
func numShareMult(a, b *numRead) {
	m := b.mult
	if a.mult != 0 {
		m = 0
		if a.big == 1 {
			m = b.big
		}
	}
	if m <= 1 || a.unit != nil && !slices.Contains(numCurrencies, a.unit) || a.date != nil || a.ver != nil || a.vague || a.v*m > b.v {
		return
	}
	round := a.nz > a.place // trailing zeros: "100~200만"
	a.v, a.place, a.nz, a.mult, a.keep, a.year4 = a.v*m, a.place*m, a.nz*m, max(a.mult, 1)*m, true, false
	a.place = numRoundPlace(a.v, a.place, a.nz, round)
}

// numRoundPlace is the place of the last digit of a number of value v, as
// a hedge reads it: from 10,000 up a round number's last nonzero digit, nz
// ("약 930만" is rounded to 10만), else the place of the last digit
// written. A plain number is read finer (numExactPlace).
func numRoundPlace(v, place, nz float64, round bool) float64 {
	if round && v >= 1e4 {
		return nz
	}
	return place
}

// numExactPlace is the place a number with no hedge word is read to, from
// its place as a hedge reads it (numRoundPlace): to its first two
// significant digits at least, so from 10,000 up the smaller of that place
// and a tenth of the place of its first digit, and below 10,000 the ones
// at least, as a zero is a digit there. "20,000" and "2만" are read to the
// thousand (19,500–20,500), "1억" to the 1,000만, "123,456" to the one,
// "2천" and "2 thousand" to the one as "2,000" is, "3.75" to the
// hundredth. The spelling never changes it, as the value alone does.
func numExactPlace(v, place float64) float64 {
	a := math.Abs(v)
	if !(a >= 1e4) { // NaN too: numSane drops it
		return min(place, 1)
	}
	return min(place, numMagnitude(a)/10)
}

// numMagnitude is the place of the first digit of a ≥ 1: 10^⌊log10 a⌋.
func numMagnitude(a float64) float64 {
	m := math.Pow(10, math.Floor(math.Log10(a)))
	switch {
	case m > a:
		m /= 10
	case m*10 <= a:
		m *= 10
	}
	return m
}

// numJoined reports whether a hyphen joins the number that ends at j to a
// group of three or more digits ("1544-0010", "010-1234-5678"): a phone or
// model number, no quantity. "2020-21" stays a year.
func numJoined(s string, j int) bool {
	if j >= len(s) || s[j] != '-' {
		return false
	}
	k := j + 1
	for k < len(s) && numIsDigit(s[k]) {
		k++
	}
	return k-(j+1) >= 3
}

// numSameScale makes the ends of a range read alike and reports whether
// they can be one range: a bare end takes the other's unit ("200~300mg",
// "3.75~4.00%"), a bare year or month the other's date ("2020~2024년",
// "9~10월"), a bare day the month of a single day ("October 27–28"), and a
// date without a year the other's year.
func numSameScale(a, b *numRead) bool {
	switch {
	case a.ver != nil || b.ver != nil:
		return false
	case a.date != nil || b.date != nil:
		if a.date == nil && !numAdoptDate(a, b.date, false) || b.date == nil && !numAdoptDate(b, a.date, true) {
			return false
		}
		if a.date.kind != b.date.kind || a.date.decade != b.date.decade {
			return false
		}
		if a.date.kind == NumDate && a.date.yearless != b.date.yearless {
			if !b.date.yearless {
				return false
			}
			d := *b.date // "2026년 9월 9일~9월 18일"
			d.y, d.yearless = a.date.y, false
			b.date = &d
		}
		return true
	case a.unit == nil && b.unit == nil:
		return true
	case a.unit == nil:
		a.unit = b.unit
		return true
	case b.unit == nil:
		b.unit = a.unit
		return true
	}
	return a.unit.kind == b.unit.kind && a.unit.class == b.unit.class
}

// numAdoptDate makes the bare number r the same kind of date as d: a year
// before a year, a month before a month, a day of d's month before or,
// when later, after a single day ("October 27–28", "27–28 October").
func numAdoptDate(r *numRead, d *numDate, later bool) bool {
	if r.unit != nil || r.vague || r.keep || r.v != math.Trunc(r.v) || r.v < 0 {
		return false
	}
	n := int(r.v)
	switch {
	case d.kind == NumYear && r.year4 && !d.decade:
		r.date = &numDate{kind: NumYear, y: n, m1: 0, m2: 11}
		return true
	case d.kind == NumDate && d.d1 == 1 && d.d2 == numMonthDays(d.m) && n >= 1 && n <= 12:
		r.date = &numDate{kind: NumDate, y: d.y, m: n, d1: 1, d2: numMonthDays(n), yearless: d.yearless}
		return true
	case d.kind == NumDate && d.d1 == d.d2 && n >= 1 && n <= numMonthDays(d.m) && (later && n > d.d2 || !later && n < d.d1):
		r.date = &numDate{kind: NumDate, y: d.y, m: d.m, d1: n, d2: n, yearless: d.yearless}
		return true
	}
	return false
}

// numRangeSep finds a range sign after a value that ends at j and returns
// where the second value starts, and the length of a minus sign before it
// ("-5~-3°C"). After a date, "부터" is one too ("9월 18일부터 10월 2일까지").
func numRangeSep(s string, j int, between, date bool) (start, sign int, ok bool) {
	if date && strings.HasPrefix(s[j:], "부터 ") {
		return numSecondStart(s, j+len("부터 "))
	}
	k := j
	for n := 0; n < 2 && k < len(s) && s[k] == ' '; n++ {
		k++
	}
	spaced := k > j
	for _, sep := range numRangeSeps {
		if !strings.HasPrefix(s[k:], sep) {
			continue
		}
		m := k + len(sep)
		switch sep {
		case "-":
			if spaced || m >= len(s) || !numIsDigit(s[m]) {
				return 0, 0, false
			}
		case "to":
			if !spaced || m >= len(s) || s[m] != ' ' {
				return 0, 0, false
			}
		}
		for n := 0; n < 2 && m < len(s) && s[m] == ' '; n++ {
			m++
		}
		return numSecondStart(s, m)
	}
	if between && spaced && strings.HasPrefix(s[k:], "and ") {
		return numSecondStart(s, k+len("and "))
	}
	return 0, 0, false
}

// numSecondStart is where the second value of a range starts at m: a
// digit, maybe after a minus sign (sign is its length), a digit after a
// currency sign ("$5–$10"), or 수 ("수만~수십만").
func numSecondStart(s string, m int) (start, sign int, ok bool) {
	for _, minus := range []string{"-", "−"} {
		if k := m + len(minus); strings.HasPrefix(s[m:], minus) && k < len(s) && numIsDigit(s[k]) {
			return k, len(minus), true
		}
	}
	if m < len(s) && numIsDigit(s[m]) {
		return m, 0, true
	}
	for _, c := range numCurrencies {
		if strings.HasPrefix(s[m:], c.word) {
			if k := m + len(c.word); k < len(s) && numIsDigit(s[k]) {
				return k, 0, true
			}
		}
	}
	if strings.HasPrefix(s[m:], "수") {
		return m, 0, true
	}
	return 0, 0, false
}

// value reads one value at i: a number with its multipliers and the unit,
// counter, currency or date it has. floor bounds the look-back. A minus
// sign ("-" or "−") right before the digits makes it negative when the
// sign starts a word: at the start, or after a space, "(", "[", ":" or "="
// ("-0.2%", "기온은 −20°C"), but not in "3-5" or "27일-28일".
func (sc *numScan) value(i, floor int) (numRead, bool) {
	r, ok := sc.unsigned(i, floor)
	if !ok || r.start != i || r.date != nil || r.ver != nil || r.vague {
		return r, ok
	}
	if n := numSignBefore(sc.s, i); n > 0 && i-n >= floor {
		r.v, r.start, r.year4 = -r.v, i-n, false
	}
	return r, ok
}

// numSignBefore is the length of a minus sign right before i that starts
// a word, or 0.
func numSignBefore(s string, i int) int {
	n := 0
	switch {
	case i >= 1 && s[i-1] == '-':
		n = 1
	case i >= len("−") && s[i-len("−"):i] == "−":
		n = len("−")
	default:
		return 0
	}
	if k := i - n; k > 0 && strings.IndexByte(" \t\n\r([:=", s[k-1]) < 0 {
		return 0
	}
	return n
}

// unsigned is value without a sign.
func (sc *numScan) unsigned(i, floor int) (numRead, bool) {
	s := sc.s
	if !numIsDigit(s[i]) {
		return sc.wordValue(i)
	}
	core, ok := numDigits(s, i)
	if !ok {
		return numRead{}, false
	}
	r := numRead{start: i, end: core.end, v: core.val, place: core.place}
	if core.comps != nil {
		return numDotted(s, r, core)
	}
	if d, end, ok := numISODate(s, core); ok {
		r.date, r.end = d, end
		return r, true
	}
	if d, start, end, ok := numDateEN(s, i, floor, core); ok {
		r.date, r.start, r.end = d, start, end
		return r, true
	}
	j := core.end
	place, nz, round := core.place, core.nz, core.whole && core.nz > 1
	if g, ok := numKo(s, core); ok {
		// 930만, 3천500만, 1억 4,960만, 1억 2천만: each group's unit is smaller.
		r.v, place, nz, round, j, r.keep, r.mult, r.big = g.v, g.place, g.nz, g.nz > g.place, g.end, true, g.mult, g.big
		for big := g.big; big > 1; {
			k := j
			if k < len(s) && s[k] == ' ' {
				k++
			}
			c2, ok := numDigits(s, k)
			if !ok || !c2.whole || c2.comps != nil {
				break
			}
			g2, ok := numKo(s, c2)
			if !ok {
				// "1만 5,000원", "1만5000원": the ones after 만, in three or
				// four digits ("1만 3일 만에" stays 1만 and 3일).
				if big == 1e4 && c2.n >= 3 && c2.val < 1e4 {
					r.v += c2.val
					place, nz, round, j = c2.place, c2.nz, c2.nz > c2.place, c2.end
				}
				break
			}
			if g2.thousandOnly && big >= 1e8 { // "1억 5천" is 1억 5천만
				g2.v, g2.place, g2.nz, g2.big = g2.v*big/1e4, g2.place*big/1e4, g2.nz*big/1e4, big/1e4
			}
			if g2.big >= big {
				break
			}
			r.v += g2.v
			place, nz, round = g2.place, g2.nz, g2.nz > g2.place
			j, big = g2.end, g2.big
		}
	} else if mult, size := numMultEN(s, j); size > 0 {
		r.v, place, nz, j, r.keep, r.mult = r.v*mult, place*mult, nz*mult, j+size, true, mult
	} else if e, x, ok := numTimesTen(s, j); ok {
		// 1.496 × 10^8 km: the power is a multiplier.
		mult := math.Pow(10, float64(x))
		r.v, place, nz, j, r.keep, r.mult = r.v*mult, place*mult, nz*mult, e, true, mult
	}
	if numTimesNumber(s, j) {
		return r, false // 1920×1080, 2560 × 1440: neither number is a quantity
	}
	j = numSkipTolerance(s, j) // 175 ± 6 cm is 175 cm
	r.keep = r.keep || core.groups
	r.nz = nz
	r.place, r.end = numRoundPlace(r.v, place, nz, round), j
	r.year4 = core.n == 4 && core.whole && !core.groups && !r.keep && r.v >= 1000 && r.v <= 2999
	// "20+" is at least 20; "10여 명" is 10 and some more.
	if j < len(s) && s[j] == '+' {
		r.plus, j = true, j+1
		r.end = j
	} else if strings.HasPrefix(s[j:], "여") {
		k := j + len("여")
		if _, _, ok := numUnitAt(s, k, false); ok || k == len(s) || s[k] == ' ' {
			r.over, j = true, k
			r.end = j
		}
	}
	if core.whole && !r.keep && !r.plus && !r.over {
		if d, end, ok := numDateKO(s, j, core); ok {
			r.date, r.end = d, end
			return r, true
		}
		if d, end, ok := numDayMonthEN(s, j, core); ok {
			r.date, r.end = d, end
			return r, true
		}
	}
	// "3분의 1": a third, exact as written (±1%).
	if strings.HasPrefix(s[j:], "분의") && core.whole && r.v > 0 && !r.keep {
		k := j + len("분의")
		if k < len(s) && s[k] == ' ' {
			k++
		}
		if c2, ok := numDigits(s, k); ok && c2.comps == nil && !c2.groups && c2.val > 0 {
			r.v = c2.val / r.v
			r.place, r.unit, r.end = r.v/50, &numFraction, c2.end
			return r, true
		}
	}
	if u, end, ok := numUnitAt(s, j, true); ok {
		r.unit, r.end = u, numPerDay(s, end, u)
		if u.kind == NumDuration {
			r = numCompound(s, r)
		}
		return r, true
	}
	// "the 1990s": a decade.
	if r.year4 && int(r.v)%10 == 0 && j < len(s) && s[j] == 's' && (j+1 == len(s) || !numIsLetter(s[j+1])) {
		r.date, r.end = &numDate{kind: NumYear, y: int(r.v), decade: true}, j+1
		return r, true
	}
	cur := numCurrencyAt(s, i, floor) // $0.05, €5, $1.5B
	if k := strings.IndexByte("KMBT", numByteAt(s, j)); (cur != nil || sc.rangeCur != nil) && !r.keep && k >= 0 && (j+1 == len(s) || !numIsLetter(s[j+1])) {
		// The suffix multiplies as "million" does, and the round rule is
		// the one of the whole value: "$50M" reads as "$50 million".
		mult := [...]float64{1e3, 1e6, 1e9, 1e12}[k]
		r.v, r.place, r.nz, r.end, r.keep, r.mult = r.v*mult, r.place*mult, r.nz*mult, j+1, true, mult
		r.place = numRoundPlace(r.v, r.place, r.nz, round)
	}
	// A letter right after the digits that starts no unit: an identifier
	// like 18e, 5G, 3D or 1st.
	if r.end == core.end && r.end < len(s) && numIsLetter(s[r.end]) {
		return r, false
	}
	if cur != nil {
		r.unit, r.start = cur, i-len(cur.word)
	}
	return r, true
}

// numTimesSign reads a times sign after a number at j, with a space before
// and after it or not: "×", "x", "*", and with dot "·" or "⋅" for powers
// of ten. It returns where what follows it starts.
func numTimesSign(s string, j int, dots bool) (int, bool) {
	k := j
	if k < len(s) && s[k] == ' ' {
		k++
	}
	switch {
	case strings.HasPrefix(s[k:], "×"):
		k += len("×")
	case k < len(s) && (s[k] == 'x' || s[k] == 'X' || s[k] == '*'):
		k++
	case dots && strings.HasPrefix(s[k:], "·"):
		k += len("·")
	case dots && strings.HasPrefix(s[k:], "⋅"):
		k += len("⋅")
	default:
		return 0, false
	}
	if k < len(s) && s[k] == ' ' {
		k++
	}
	return k, true
}

// numTimesTen reads a power of ten after a number at j, "× 10^8",
// "x10^-19", "×10^{23}" or "× 10⁸": it returns where it ends and the power.
func numTimesTen(s string, j int) (end, power int, ok bool) {
	k, ok := numTimesSign(s, j, true)
	if !ok || !strings.HasPrefix(s[k:], "10") {
		return 0, 0, false
	}
	k += 2
	neg, digits := false, 0
	if k < len(s) && s[k] == '^' {
		k++
		brace := k < len(s) && s[k] == '{'
		if brace {
			k++
		}
		switch {
		case strings.HasPrefix(s[k:], "-"), strings.HasPrefix(s[k:], "+"):
			neg, k = s[k] == '-', k+1
		case strings.HasPrefix(s[k:], "−"):
			neg, k = true, k+len("−")
		}
		p, e, ok := numSmall(s, k, 3)
		if !ok || brace && !strings.HasPrefix(s[e:], "}") {
			return 0, 0, false
		}
		power, k = p, e
		if brace {
			k++
		}
	} else {
		for k < len(s) && digits < 3 {
			r, size := utf8.DecodeRuneInString(s[k:])
			if d := numSuperDigit(r); d >= 0 {
				power, digits = power*10+d, digits+1
			} else if r == '⁻' && digits == 0 && !neg {
				neg = true
			} else {
				break
			}
			k += size
		}
		if digits == 0 {
			return 0, 0, false
		}
	}
	if k < len(s) && numIsDigit(s[k]) {
		return 0, 0, false
	}
	if neg {
		power = -power
	}
	return k, power, true
}

// numSuperDigit is the value of a superscript digit, or -1.
func numSuperDigit(r rune) int {
	switch {
	case r == '⁰':
		return 0
	case r == '¹':
		return 1
	case r == '²', r == '³':
		return int(r-'²') + 2
	case r >= '⁴' && r <= '⁹':
		return int(r-'⁴') + 4
	}
	return -1
}

// numTimesNumber reports whether a times sign and another number follow a
// number that ends at j: "1920×1080", "2560 × 1440", "3 x 4". Neither is a
// quantity of its own (numAfterTimes).
func numTimesNumber(s string, j int) bool {
	k, ok := numTimesSign(s, j, false)
	return ok && k < len(s) && numIsDigit(s[k])
}

// numAfterTimes reports whether the digit at i follows a number and a
// times sign: the second number of "1920×1080" starts no quantity.
func numAfterTimes(s string, i int) bool {
	k := i
	if k > 0 && s[k-1] == ' ' {
		k--
	}
	switch {
	case k >= 2 && s[k-2:k] == "×":
		k -= 2
	case k >= 1 && (s[k-1] == 'x' || s[k-1] == 'X' || s[k-1] == '*'):
		k--
	default:
		return false
	}
	if k > 0 && s[k-1] == ' ' {
		k--
	}
	return k > 0 && numIsDigit(s[k-1])
}

// numSkipTolerance skips a tolerance after a number at j: "175 ± 6 cm" is
// 175 cm.
func numSkipTolerance(s string, j int) int {
	k := j
	if k < len(s) && s[k] == ' ' {
		k++
	}
	if !strings.HasPrefix(s[k:], "±") {
		return j
	}
	k += len("±")
	if k < len(s) && s[k] == ' ' {
		k++
	}
	if d, ok := numDigits(s, k); ok && d.comps == nil {
		return d.end
	}
	return j
}

// numCompound reads the smaller units after a duration: "8분 20초",
// "1시간 30분", "2 hours 30 minutes". The value is then in the last unit,
// to that unit's last digit.
func numCompound(s string, r numRead) numRead {
	for {
		k := r.end
		if k < len(s) && s[k] == ' ' {
			k++
		}
		if k >= len(s) || !numIsDigit(s[k]) {
			return r
		}
		c, ok := numDigits(s, k)
		if !ok || c.comps != nil || c.groups {
			return r
		}
		u, e, ok := numUnitAt(s, c.end, true)
		if !ok || u.kind != NumDuration || u.scale >= r.unit.scale {
			return r
		}
		// Its last nonzero digit is the last part's, unless that is 0
		// ("2시간 0분" has the 2).
		nz := r.nz * (r.unit.scale / u.scale)
		if c.val > 0 {
			nz = c.nz
		}
		r.v = r.v*(r.unit.scale/u.scale) + c.val
		r.place, r.nz, r.unit, r.end = c.place, nz, u, e
	}
}

// numCurrencyAt is the currency written right before the digit at i.
func numCurrencyAt(s string, i, floor int) *numUnit {
	for _, c := range numCurrencies {
		k := i - len(c.word)
		if k < floor || k < 0 || s[k:i] != c.word {
			continue
		}
		if k > 0 && numIsLetter(s[k-1]) {
			continue
		}
		return c
	}
	return nil
}

// numUnitAt finds the unit at j, after at most one space when spaced, and
// returns where it ends.
func numUnitAt(s string, j int, spaced bool) (*numUnit, int, bool) {
	k := j
	if spaced {
		switch {
		case k < len(s) && s[k] == ' ':
			k++
		case strings.HasPrefix(s[k:], "\u00a0"):
			k += len("\u00a0")
		case strings.HasPrefix(s[k:], "\u202f"):
			k += len("\u202f")
		}
	}
	if k >= len(s) {
		return nil, j, false
	}
	for _, u := range numUnitIndex[s[k]] {
		if !strings.HasPrefix(s[k:], u.word) {
			continue
		}
		e := k + len(u.word)
		last := u.word[len(u.word)-1]
		switch {
		case numIsLetter(last):
			if e < len(s) && (numIsLetter(s[e]) || numIsDigit(s[e])) {
				continue
			}
			if (u.word == "x" || u.word == "-fold") && k != j { // "10 x 20" is no ratio
				continue
			}
			if u.word == "times" && numTimesTail(s, e) {
				return &numTimesCount, e, true
			}
		case last >= utf8.RuneSelf && numKoWordAfter(s, e):
			continue // a Korean unit must end its word: 3원 yes, 3원칙 no
		}
		switch u.word {
		case "초":
			if strings.HasPrefix(s[e:], "과") || strings.HasPrefix(s[e:], "반") { // 초과 is a limit
				continue
			}
		case "분":
			if strings.HasPrefix(s[e:], "의") || strings.HasPrefix(s[e:], "기") {
				continue
			}
		}
		return u, e, true
	}
	return nil, j, false
}

// numTimesTail reports whether "times" at e is followed by a, per or each:
// how often, not how many times as much.
func numTimesTail(s string, e int) bool {
	return strings.HasPrefix(s[e:], " a ") || strings.HasPrefix(s[e:], " per ") || strings.HasPrefix(s[e:], " each ")
}

// numPerDay skips a per-day rate after an amount ("mg/일", "mg/day", "per
// day"): an amount a day reads as the amount.
func numPerDay(s string, e int, u *numUnit) int {
	if u.kind != NumMeasure && u.kind != NumMoney && u.kind != NumCount {
		return e
	}
	for _, w := range []string{"/일", "/day", "/d", " per day", " a day", "/하루"} {
		if !strings.HasPrefix(s[e:], w) {
			continue
		}
		if k := e + len(w); k == len(s) || !numIsLetter(s[k]) {
			return k
		}
	}
	return e
}

// numKoWordAfter reports whether a Korean word goes on at e with a
// syllable that is no particle or ending of a number.
func numKoWordAfter(s string, e int) bool {
	if e >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[e:])
	if !numIsHangul(r) {
		return false
	}
	for _, t := range numKoTails {
		if strings.HasPrefix(s[e:], t) {
			return false
		}
	}
	return true
}

// before reads the hedges and limits before the value that starts at i
// and returns where the quantity starts.
func (sc *numScan) before(i int) (start int, hedged, lower, upper bool) {
	s, floor := sc.s, sc.last
	start = i
	for n := 0; n < 3; n++ {
		k := numBackSpace(s, start, floor)
		var w, wl, wu int
		if !lower && !upper {
			// The longer word wins: "no more than" holds the "more than" of
			// the other side, "not exceeding" its "exceeding".
			wl, wu = numWordBefore(s, k, floor, numLowerBefore), numWordBefore(s, k, floor, numUpperBefore)
		}
		switch {
		case !hedged && numWordBefore(s, start, floor, numTildes) > 0:
			hedged, w, k = true, numWordBefore(s, start, floor, numTildes), start
		case !hedged && numWordBefore(s, k, floor, numHedgesBefore) > 0:
			hedged, w = true, numWordBefore(s, k, floor, numHedgesBefore)
		case wu > wl:
			upper, w = true, wu
		case wl > 0:
			lower, w = true, wl
		default:
			return start, hedged, lower, upper
		}
		start = k - w
	}
	return start, hedged, lower, upper
}

// numBackSpace steps back over spaces and emphasis marks from k, not past
// floor.
func numBackSpace(s string, k, floor int) int {
	for k > floor {
		switch {
		case s[k-1] == ' ' || s[k-1] == '*' || s[k-1] == '_':
			k--
		case k-2 >= floor && s[k-2:k] == "\u00a0":
			k -= 2
		default:
			return k
		}
	}
	return k
}

// numWordBefore is the length of the longest word of words that ends at k
// with a boundary before it, not past floor, or 0. English words match in
// any case: a unit often starts with one ("About 8 minutes", "Up to 3").
func numWordBefore(s string, k, floor int, words []string) int {
	best := 0
	for _, w := range words {
		b := k - len(w)
		if len(w) <= best || b < floor || b < 0 || !numSameWord(s[b:k], w) {
			continue
		}
		if b > 0 {
			switch {
			case numIsLetter(w[0]):
				if numIsLetter(s[b-1]) || numIsDigit(s[b-1]) {
					continue
				}
			case w[0] >= utf8.RuneSelf:
				if r, _ := utf8.DecodeLastRuneInString(s[:b]); numIsHangul(r) {
					continue
				}
			}
		}
		best = len(w)
	}
	return best
}

// numSameWord reports whether s is the word w: as written, or for an
// English word with its letters in another case.
func numSameWord(s, w string) bool {
	if s == w {
		return true
	}
	if len(s) != len(w) || !numIsLetter(w[0]) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != w[i] && !(numIsLetter(s[i]) && numIsLetter(w[i]) && s[i]|0x20 == w[i]|0x20) {
			return false
		}
	}
	return true
}

// numAfter reads the hedges and limits after a quantity that ends at j,
// and whether 남짓 says "and some more" (over).
func numAfter(s string, j int) (end int, hedged, lower, upper, over bool) {
	end = j
	for n := 0; n < 3; n++ {
		k := end
		if k < len(s) && s[k] == ' ' {
			k++
		}
		if w := numWordAfter(s, k, numHedgesAfter); w > 0 && !hedged {
			hedged, end = true, k+w
			continue
		}
		if w := numWordAfter(s, k, numOverAfter); w > 0 && !over {
			over, end = true, k+w
			continue
		}
		if !lower && !upper {
			if w := numWordAfter(s, k, numLowerAfter); w > 0 {
				lower, end = true, k+w
				continue
			}
			if w := numWordAfter(s, k, numUpperAfter); w > 0 {
				upper, end = true, k+w
				continue
			}
		}
		// 경 and 께 attach: 1월경, 1877년경.
		if !hedged && (strings.HasPrefix(s[end:], "경") || strings.HasPrefix(s[end:], "께")) &&
			!numKoWordAfter(s, end+len("경")) {
			hedged, end = true, end+len("경")
			continue
		}
		break
	}
	return end, hedged, lower, upper, over
}

// numWordAfter is the length of the word of words that starts at k and
// ends its word, or 0.
func numWordAfter(s string, k int, words []string) int {
	for _, w := range words {
		if !strings.HasPrefix(s[k:], w) {
			continue
		}
		e := k + len(w)
		last := w[len(w)-1]
		switch {
		case numIsLetter(last) && e < len(s) && numIsLetter(s[e]):
			continue
		case last >= utf8.RuneSelf && numKoWordAfter(s, e):
			continue
		case numIsLetter(w[0]) && w[0] != 'i' && (k == 0 || s[k-1] != ' '): // "or more" after a space
			continue
		}
		return len(w)
	}
	return 0
}

// wordValue reads a value that starts with a letter: a version (v2.1) or a
// number written as a word ("thousands", "수천").
func (sc *numScan) wordValue(i int) (numRead, bool) {
	s := sc.s
	r := numRead{start: i}
	if (s[i] == 'v' || s[i] == 'V') && i+1 < len(s) && numIsDigit(s[i+1]) {
		c, ok := numDigits(s, i+1)
		if !ok || c.end < len(s) && numIsLetter(s[c.end]) {
			return r, false
		}
		switch {
		case c.comps != nil:
			r.ver = c.comps
		case !c.whole:
			whole := math.Trunc(c.val)
			r.ver = []int{int(whole), int(math.Round((c.val - whole) / c.place))}
		default:
			r.ver = []int{int(c.val)}
		}
		r.end = c.end
		return r, true
	}
	for _, w := range numVagueEN {
		if !strings.HasPrefix(s[i:], w.word) || i+len(w.word) < len(s) && numIsLetter(s[i+len(w.word)]) {
			continue
		}
		r.vague, r.v, r.vHi, r.end = true, w.lo, w.hi, i+len(w.word)
		if strings.HasPrefix(s[r.end:], " of ") {
			if u, e, ok := numUnitAt(s, r.end+len(" of"), true); ok {
				r.unit, r.end = u, e
			}
		}
		return r, true
	}
	for _, w := range numVagueKO {
		if !strings.HasPrefix(s[i:], w.word) {
			continue
		}
		if p, _ := utf8.DecodeLastRuneInString(s[:i]); i > 0 && numIsHangul(p) {
			return r, false
		}
		r.vague, r.v, r.vHi, r.end = true, w.lo, w.hi, i+len(w.word)
		if u, e, ok := numUnitAt(s, r.end, true); ok {
			r.unit, r.end = u, e
		} else if numKoWordAfter(s, r.end) {
			return r, false
		}
		return r, true
	}
	return r, false
}

// numDotted reads a number with three or more dotted parts: a date
// (2026.10.16) or a version (1.99.1).
func numDotted(s string, r numRead, c numCore) (numRead, bool) {
	p := c.comps
	r.end = c.end
	if len(p) == 3 && p[0] >= 1000 && p[0] <= 2999 && p[1] >= 1 && p[1] <= 12 && p[2] >= 1 && p[2] <= numMonthDays(p[1]) {
		r.date = &numDate{kind: NumDate, y: p[0], m: p[1], d1: p[2], d2: p[2]}
		return r, true
	}
	if r.end < len(s) && numIsLetter(s[r.end]) {
		return r, false
	}
	r.ver = p
	return r, true
}

// numISODate reads 2026-10-16, 2026/10/16 and "2026. 10. 16.".
func numISODate(s string, c numCore) (*numDate, int, bool) {
	if c.n != 4 || !c.whole || c.groups || c.val < 1000 || c.val > 2999 || c.end >= len(s) {
		return nil, 0, false
	}
	sep := s[c.end]
	if sep != '-' && sep != '/' && sep != '.' {
		return nil, 0, false
	}
	k := c.end + 1
	spaced := sep == '.' && k < len(s) && s[k] == ' '
	if spaced {
		k++
	}
	m, k2, ok := numSmall(s, k, 2)
	if !ok || k2 >= len(s) || s[k2] != sep || m < 1 || m > 12 {
		return nil, 0, false
	}
	k = k2 + 1
	if spaced {
		if k >= len(s) || s[k] != ' ' {
			return nil, 0, false
		}
		k++
	}
	d, k3, ok := numSmall(s, k, 2)
	if !ok || d < 1 || d > numMonthDays(m) {
		return nil, 0, false
	}
	if spaced && k3 < len(s) && s[k3] == '.' {
		k3++
	}
	return &numDate{kind: NumDate, y: int(c.val), m: m, d1: d, d2: d}, k3, true
}

// numSmall reads one to n digits at k that no other digit follows.
func numSmall(s string, k, n int) (int, int, bool) {
	v, j := 0, k
	for j < len(s) && numIsDigit(s[j]) && j-k < n {
		v = v*10 + int(s[j]-'0')
		j++
	}
	return v, j, j > k && (j == len(s) || !numIsDigit(s[j]))
}

// numDateKO reads a Korean date or year after the number of c, from j:
// "2026년", "2026년 9월 18일", "9월 18일", "10월 27~28일", "2024년 말",
// "2026년 2분기", "1990년대".
func numDateKO(s string, j int, c numCore) (*numDate, int, bool) {
	n := int(c.val)
	switch {
	case strings.HasPrefix(s[j:], "년") && c.n == 4 && n >= 1000 && n <= 2999:
		k := j + len("년")
		if strings.HasPrefix(s[k:], "대") && !numKoWordAfter(s, k+len("대")) {
			return &numDate{kind: NumYear, y: n, decade: true}, k + len("대"), true
		}
		if numKoWordAfter(s, k) {
			return nil, 0, false
		}
		if k+1 < len(s) && s[k] == ' ' {
			if m, k2, ok := numSmall(s, k+1, 2); ok && m >= 1 && m <= 12 && strings.HasPrefix(s[k2:], "월") &&
				!numKoWordAfter(s, k2+len("월")) {
				d, end := numMonthRest(s, k2+len("월"), m)
				d.y, d.yearless = n, false
				return d, end, true
			}
		}
		m1, m2, end := numYearPart(s, k)
		return &numDate{kind: NumYear, y: n, m1: m1, m2: m2}, end, true
	case strings.HasPrefix(s[j:], "월") && c.n <= 2 && n >= 1 && n <= 12:
		k := j + len("월")
		if numKoWordAfter(s, k) {
			return nil, 0, false
		}
		d, end := numMonthRest(s, k, n)
		return d, end, true
	}
	return nil, 0, false
}

// numMonthRest reads what follows "N월" at k: a day ("18일"), days
// ("27~28일") or a part of the month (초, 중순, 말). The date has no year.
func numMonthRest(s string, k, m int) (*numDate, int) {
	d := &numDate{kind: NumDate, m: m, d1: 1, d2: numMonthDays(m), yearless: true}
	if k+1 >= len(s) || s[k] != ' ' {
		return d, k
	}
	if d1, k2, ok := numSmall(s, k+1, 2); ok && d1 >= 1 && d1 <= numMonthDays(m) {
		// "18일", "27~28일", "27일~28일", "27일-28일"
		e := k2
		if strings.HasPrefix(s[k2:], "일") && !numKoWordAfter(s, k2+len("일")) {
			d.d1, d.d2, e = d1, d1, k2+len("일")
		}
		for _, sep := range []string{"~", "∼", "〜", "～", "–", "-"} {
			if !strings.HasPrefix(s[e:], sep) {
				continue
			}
			if d2, k3, ok := numSmall(s, e+len(sep), 2); ok && d2 > d1 && d2 <= numMonthDays(m) &&
				strings.HasPrefix(s[k3:], "일") && !numKoWordAfter(s, k3+len("일")) {
				d.d1, d.d2 = d1, d2
				return d, k3 + len("일")
			}
		}
		if e > k2 {
			return d, e
		}
	}
	for _, p := range []struct {
		word   string
		d1, d2 int
	}{{"초순", 1, 10}, {"상순", 1, 10}, {"중순", 11, 20}, {"하순", 21, 31}, {"초", 1, 10}, {"말", 21, 31}} {
		if strings.HasPrefix(s[k+1:], p.word) && !numKoWordAfter(s, k+1+len(p.word)) {
			d.d1, d.d2 = p.d1, min(p.d2, numMonthDays(m))
			return d, k + 1 + len(p.word)
		}
	}
	return d, k
}

// numYearPart reads a part of a year after "N년" at k: 초, 말, 상반기,
// 하반기, N분기 or a season. It returns the months it spans (0 = January).
func numYearPart(s string, k int) (m1, m2, end int) {
	if k+1 >= len(s) || s[k] != ' ' {
		return 0, 11, k
	}
	if q, k2, ok := numSmall(s, k+1, 1); ok && q >= 1 && q <= 4 && strings.HasPrefix(s[k2:], "분기") {
		return (q - 1) * 3, q*3 - 1, k2 + len("분기")
	}
	for _, p := range []struct {
		word   string
		m1, m2 int
	}{
		{"상반기", 0, 5}, {"하반기", 6, 11}, {"중반", 4, 7}, {"연말", 9, 11}, {"초", 0, 2}, {"말", 9, 11},
		{"봄", 2, 4}, {"여름", 5, 7}, {"가을", 8, 10}, {"겨울", 11, 13},
	} {
		if strings.HasPrefix(s[k+1:], p.word) && !numKoWordAfter(s, k+1+len(p.word)) {
			return p.m1, p.m2, k + 1 + len(p.word)
		}
	}
	return 0, 11, k
}

// numDateEN reads an English date whose month name stands before the number
// at i: "October 25, 2026", "Oct. 25", "October 2026". It returns where
// the date starts.
func numDateEN(s string, i, floor int, c numCore) (*numDate, int, int, bool) {
	if !c.whole || c.groups || i-1 < floor || s[i-1] != ' ' {
		return nil, 0, 0, false
	}
	k := i - 1
	if k > floor && s[k-1] == '.' {
		k--
	}
	m, start := 0, 0
	for _, mn := range numMonths {
		b := k - len(mn.word)
		if b >= floor && b >= 0 && s[b:k] == mn.word && (b == 0 || !numIsLetter(s[b-1])) {
			m, start = mn.m, b
			break
		}
	}
	if m == 0 {
		return nil, 0, 0, false
	}
	n := int(c.val)
	if c.n == 4 && n >= 1000 && n <= 2999 { // October 2026
		return &numDate{kind: NumDate, y: n, m: m, d1: 1, d2: numMonthDays(m)}, start, c.end, true
	}
	if c.n > 2 || n < 1 || n > numMonthDays(m) {
		return nil, 0, 0, false
	}
	end := c.end
	for _, suf := range []string{"st", "nd", "rd", "th"} {
		if strings.HasPrefix(s[end:], suf) && (end+2 == len(s) || !numIsLetter(s[end+2])) {
			end += 2
			break
		}
	}
	d := &numDate{kind: NumDate, m: m, d1: n, d2: n, yearless: true}
	if y, e, ok := numYearAfter(s, end); ok {
		d.y, d.yearless, end = y, false, e
	}
	return d, start, end, true
}

// numDayMonthEN reads "25 October 2026" or "25 Oct" after a day that ends
// at j.
func numDayMonthEN(s string, j int, c numCore) (*numDate, int, bool) {
	if c.n > 2 || j >= len(s) || s[j] != ' ' {
		return nil, 0, false
	}
	n := int(c.val)
	for _, mn := range numMonths {
		if !strings.HasPrefix(s[j+1:], mn.word) {
			continue
		}
		e := j + 1 + len(mn.word)
		if e < len(s) && s[e] == '.' {
			e++
		} else if e < len(s) && numIsLetter(s[e]) {
			continue
		}
		if n < 1 || n > numMonthDays(mn.m) {
			return nil, 0, false
		}
		d := &numDate{kind: NumDate, m: mn.m, d1: n, d2: n, yearless: true}
		if y, e2, ok := numYearAfter(s, e); ok {
			d.y, d.yearless, e = y, false, e2
		}
		return d, e, true
	}
	return nil, 0, false
}

// numYearAfter reads ", 2026" or " 2026" at e.
func numYearAfter(s string, e int) (int, int, bool) {
	k := e
	if k < len(s) && s[k] == ',' {
		k++
	}
	if k >= len(s) || s[k] != ' ' {
		return 0, 0, false
	}
	y, k2, ok := numSmall(s, k+1, 4)
	if !ok || k2-(k+1) != 4 || y < 1000 || y > 2999 || k2 < len(s) && numIsLetter(s[k2]) {
		return 0, 0, false
	}
	return y, k2, true
}

// numMonthDays is the most days month m has; February has 29 so that
// "2월 29일" reads.
func numMonthDays(m int) int {
	switch m {
	case 2:
		return 29
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// numDaysIn is the number of days of month m in year y.
func numDaysIn(y, m int) int {
	if m == 2 && !(y%4 == 0 && (y%100 != 0 || y%400 == 0)) {
		return 28
	}
	return numMonthDays(m)
}

// numCivil counts the days from 1970-01-01 to y-m-d (days_from_civil, by
// Howard Hinnant), for years 1 and later.
func numCivil(y, m, d int) float64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	yoe := y - era*400
	doy := (153*((m+9)%12)+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return float64(era*146097 + doe - 719468)
}

// numCumDays are the days of a leap year before each month, so that every
// date without a year has a day of its own: 2월 29일 is not 3월 1일.
var numCumDays = [13]int{0, 0, 31, 60, 91, 121, 152, 182, 213, 244, 274, 305, 335}

// numDayOfYear is the day of a leap year, January 1 being 0.
func numDayOfYear(m, d int) float64 { return float64(numCumDays[m] + d - 1) }

// numDateSpan is the span of a date in days, or of a year in months:
// [lo, hi).
func numDateSpan(d *numDate) (float64, float64) {
	switch {
	case d.kind == NumYear && d.decade:
		n := 10
		if d.y%100 == 0 && d.y != 2000 { // 1700년대, the 1800s: a century; 2000년대 is the decade
			n = 100
		}
		return float64(d.y * 12), float64((d.y + n) * 12)
	case d.kind == NumYear:
		return float64(d.y*12 + d.m1), float64(d.y*12 + d.m2 + 1)
	case d.yearless:
		return numDayOfYear(d.m, d.d1), numDayOfYear(d.m, d.d2) + 1
	}
	last := numDaysIn(d.y, d.m)
	return numCivil(d.y, d.m, min(d.d1, last)), numCivil(d.y, d.m, min(d.d2, last)) + 1
}

// numDateGrain is how far a hedge (경, 쯤, around) widens a date: a day for
// a day, five days for part of a month, half a month for a month, a season
// for part of a year and a year for a year.
func numDateGrain(d *numDate) float64 {
	switch {
	case d.kind == NumYear && !d.decade && d.m2-d.m1 < 11:
		return 3
	case d.kind == NumYear:
		return 12
	case d.d1 == d.d2:
		return 1
	case d.d2-d.d1 < 20:
		return 5
	}
	return 15
}

// numBuild makes the Quantity of a value or a range from start to end. A
// bare number is kept only as a year (four digits) or as a count written
// with separators or a multiplier.
func numBuild(s string, start, end int, a, b numRead, isRange, hedged, lower, upper bool) (Quantity, bool) {
	if a.unit == nil && b.unit == nil && a.date == nil && a.ver == nil && a.year4 && b.year4 {
		a.date = &numDate{kind: NumYear, y: int(a.v), m1: 0, m2: 11}
		b.date = &numDate{kind: NumYear, y: int(b.v), m1: 0, m2: 11}
	}
	q := Quantity{Start: start, End: end, Text: s[start:end]}
	v := &q.num
	switch {
	case a.ver != nil:
		q.Kind, v.ver = NumVersion, a.ver
	case a.date != nil:
		q.Kind = a.date.kind
		alo, ahi := numDateSpan(a.date)
		blo, bhi := numDateSpan(b.date)
		if hedged {
			ga, gb := numDateGrain(a.date), numDateGrain(b.date)
			alo, ahi, blo, bhi = alo-ga, ahi+ga, blo-gb, bhi+gb
		}
		v.lo, v.loHi, v.hiLo, v.hi = alo, ahi, blo, bhi
		v.nomLo, v.nomHi = alo, bhi
		if q.Kind == NumDate {
			v.yearless, v.dayLo, v.dayHi = a.date.yearless, 0, 366
			if !a.date.yearless && a.date.y == b.date.y {
				v.dayLo = numDayOfYear(a.date.m, a.date.d1)
				v.dayHi = numDayOfYear(b.date.m, b.date.d2) + 1
				if hedged {
					v.dayLo, v.dayHi = v.dayLo-numDateGrain(a.date), v.dayHi+numDateGrain(b.date)
				}
			}
		}
	default:
		u := a.unit
		if u == nil {
			u = b.unit
		}
		if u == nil {
			if !a.keep && !b.keep && !a.vague && !b.vague {
				return Quantity{}, false
			}
			u = &numBareCount
		}
		q.Kind, v.class = u.kind, u.class
		alo, ahi, anom := numEnd(a, u, hedged)
		blo, bhi, bnom := numEnd(b, u, hedged)
		if a.vague {
			anom = alo
		}
		if b.vague {
			bnom = bhi
		}
		v.lo, v.loHi, v.hiLo, v.hi = numSnap(min(alo, blo)), numSnap(ahi), numSnap(blo), numSnap(max(ahi, bhi))
		v.nomLo, v.nomHi = numSnap(anom), numSnap(bnom)
	}
	if lower || upper {
		v.shape, v.upper = numLimit, !lower
		if isRange {
			v.shape = numRangeLimit
		}
	}
	return q, numSane(v)
}

// numEnd is one end of a value in the base unit: its interval and its
// value as written.
func numEnd(r numRead, u *numUnit, hedged bool) (lo, hi, nom float64) {
	if r.vague {
		lo, hi = (r.v+u.offset)*u.scale, (r.vHi+u.offset)*u.scale
		return lo, hi, lo
	}
	nom = (r.v + u.offset) * u.scale
	scale := math.Abs(u.scale)
	switch {
	case r.over: // "10여", "100명 남짓", "2%대": from the value up to its next step, 10–20, 100–200, 2–3%
		w := 0.0
		if hedged {
			w = r.place * scale
		}
		return nom - w, nom + r.nz*scale, nom
	case hedged: // a whole unit of its last digit
		w := r.place * scale
		return nom - w, nom + w, nom
	}
	w := numExactPlace(r.v, r.place) / 2 * scale // half a unit, to two digits at least
	return nom - w, nom + w, nom
}

// numSnap rounds v to 15 significant digits, so that one end reached two
// ways is one float: "1.1%" ends at 1.1 + 0.05 and "1.2%" starts at 1.2 −
// 0.05, which differ in their last bits, and ends that only touch must
// not overlap.
func numSnap(v float64) float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	switch e := int(math.Floor(math.Log10(math.Abs(v)))) - 14; {
	case e < 0 && e >= -22:
		p := math.Pow10(-e)
		return math.Round(v*p) / p
	case e > 0 && e <= 22:
		p := math.Pow10(e)
		return math.Round(v/p) * p
	}
	return v
}

// numSane reports whether a value is usable: finite, with its ends in
// order.
func numSane(v *numValue) bool {
	for _, f := range []float64{v.lo, v.loHi, v.hiLo, v.hi, v.nomLo, v.nomHi, v.dayLo, v.dayHi} {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	if v.ver != nil {
		return true
	}
	return v.lo < v.loHi && v.loHi <= v.hi && v.lo <= v.hiLo && v.hiLo < v.hi &&
		v.lo <= v.nomLo && v.nomLo <= v.nomHi && v.nomHi <= v.hi
}

// numComparable reports whether a and b say how much of one thing: the same
// kind and class, where a count without a counter goes with any counter.
func numComparable(a, b Quantity) bool {
	switch {
	case a.Kind != b.Kind:
		return false
	case a.Kind == NumCount:
		return a.num.class == b.num.class || a.num.class == "" || b.num.class == ""
	}
	return a.num.class == b.num.class
}

// numOverlap reports whether two intervals share more than an end.
func numOverlap(alo, ahi, blo, bhi float64) bool { return alo < bhi && blo < ahi }

// Compatible reports whether a and b can be the same value: the same kind
// and overlapping intervals. Two one-sided limits fit only with the same
// direction and the same limit value, a limit given as a range only the
// same range, and a point fits a limit it satisfies. From P0-6: "약 8분
// 20초" fits "8분 19초" and "약 930만" fits "9,279,861명"; "약 8분 10초" and
// "약 8분 3초", "970만 안팎" and "약 956만", "200~300mg/일 이하" and "200mg
// 미만", "9월 18일" and "10월 23일" do not.
func Compatible(a, b Quantity) bool {
	if !numComparable(a, b) {
		return false
	}
	x, y := a.num, b.num
	if a.Kind == NumVersion {
		return numVersionFits(x, y)
	}
	if a.Kind == NumDate && x.yearless != y.yearless {
		if x.yearless { // a date without a year fits one on the same days
			x, y = y, x
		}
		return numOverlap(x.dayLo, x.dayHi, y.lo, y.hi)
	}
	if x.shape > y.shape {
		x, y = y, x
	}
	switch {
	case x.shape == numPoint && y.shape == numPoint:
		return numOverlap(x.lo, x.hi, y.lo, y.hi)
	case x.shape == numPoint && y.shape == numLimit:
		// The value as written satisfies the limit, within the limit
		// value's own width.
		if y.upper {
			return x.nomHi < y.hi
		}
		return x.nomLo > y.lo
	case x.shape == numPoint && y.shape == numRangeLimit:
		// "200~300mg/일" is the limit "200~300mg/일 이하" gives; a single
		// value is not.
		return x.nomLo < x.nomHi && numSameRange(x, y)
	case x.shape == numLimit && y.shape == numLimit:
		return x.upper == y.upper && numOverlap(x.lo, x.hi, y.lo, y.hi)
	case x.shape == numRangeLimit && y.shape == numRangeLimit:
		return x.upper == y.upper && numSameRange(x, y)
	}
	return false // a one-sided limit and a limit given as a range
}

// numSameRange reports whether x and y are the same range: both ends fit.
func numSameRange(x, y numValue) bool {
	return numOverlap(x.lo, x.loHi, y.lo, y.loHi) && numOverlap(x.hiLo, x.hi, y.hiLo, y.hi)
}

// numVersionFits compares versions: values fit when one is the other with
// more parts (3.15 and 3.15.2), a value and a limit (3.10 이상) when the
// value holds.
func numVersionFits(x, y numValue) bool {
	if x.shape > y.shape {
		x, y = y, x
	}
	switch {
	case x.shape == numPoint && y.shape == numPoint:
		for i := 0; i < min(len(x.ver), len(y.ver)); i++ {
			if x.ver[i] != y.ver[i] {
				return false
			}
		}
		return true
	case x.shape == numPoint && y.shape == numLimit:
		c := numVersionCmp(x.ver, y.ver)
		return y.upper && c <= 0 || !y.upper && c >= 0
	case x.shape == numLimit && y.shape == numLimit:
		return x.upper == y.upper && numVersionCmp(x.ver, y.ver) == 0
	}
	return false
}

// numVersionCmp compares versions part by part, a missing part as 0.
func numVersionCmp(a, b []int) int {
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

// Mismatch reports whether the quantities of two texts disagree: they share
// a kind (with its class: a measure's dimension, a currency, % or %p) and
// no quantity of it in a is Compatible with one of it in b. Counts are
// judged per counter both texts have, a count without a counter joining
// each (so 25개 that fit do not hide 970만 명 against 956만 명), or as one
// group when the texts share no counter. A text without a quantity of a
// kind never disagrees on it, since a number left out is not a different
// number. It drives the numberMismatch and numbersAbsent checks and
// Item.NumbersDiffer.
func Mismatch(a, b []Quantity) bool {
	return numMatchFor(b, len(a)).mismatch(a)
}

// Unmatched returns the indexes, in order, of the quantities in a whose
// kind (and class) b also has and which are Compatible with none of b's
// quantities: the numbers the phone draws bold on a's side of a 다른 말
// item.
func Unmatched(a, b []Quantity) []int {
	return numMatchFor(b, len(a)).unmatched(a)
}

// numScanMost bounds a scan: a text of more quantities that more than as
// many quantities are compared with is indexed, so a unit is compared with
// a whole answer of thousands, or two whole answers with each other, in
// time near their size and not its square.
const numScanMost = 32

// numMatcher compares quantities with those of one text, b.
type numMatcher struct {
	b  []Quantity
	ix *numIndex // b indexed, when it is large and often compared with
}

// numMatchFor gets b ready for comparing queries quantities with it in
// all: an index costs a sort of b, so a few are compared by a scan.
func numMatchFor(b []Quantity, queries int) numMatcher {
	m := numMatcher{b: b}
	if len(b) > numScanMost && queries > numScanMost {
		m.ix = numIndexOf(b)
	}
	return m
}

// mismatch is Mismatch(a, m.b).
func (m numMatcher) mismatch(a []Quantity) bool {
	switch {
	case len(a) == 0 || len(m.b) == 0:
		return false
	case m.ix != nil:
		return numMismatchIndex(a, m.ix)
	}
	return numMismatchScan(a, m.b)
}

// unmatched is Unmatched(a, m.b).
func (m numMatcher) unmatched(a []Quantity) []int {
	var out []int
	for i, x := range a {
		if compared, fits := m.compare(x); compared && !fits {
			out = append(out, i)
		}
	}
	return out
}

// compare reports whether m.b has a quantity x is compared with
// (numComparable), and one that is Compatible with x.
func (m numMatcher) compare(x Quantity) (compared, fits bool) {
	if m.ix != nil {
		st := m.ix.set(x)
		return st != nil, st != nil && st.fits(x)
	}
	for _, y := range m.b {
		if numComparable(x, y) {
			if Compatible(x, y) {
				return true, true
			}
			compared = true
		}
	}
	return compared, false
}

// numKey is the kind and class of a quantity: what it is compared with,
// but that a count without a counter goes with every counter.
type numKey struct{ kind, class string }

func numKeyOf(q Quantity) numKey { return numKey{q.Kind, q.num.class} }

// numMismatchScan is Mismatch by comparing pairs, for a b with few
// quantities.
func numMismatchScan(a, b []Quantity) bool {
	var done []numKey
	for i, x := range a {
		k := numKeyOf(x)
		if k.kind == NumCount || slices.Contains(done, k) {
			continue // a count, or a group done before
		}
		done = append(done, k)
		compared, fits := false, false
		for _, x2 := range a[i:] {
			for _, y := range b {
				if numKeyOf(x2) == k && numKeyOf(y) == k {
					compared, fits = true, Compatible(x2, y)
					if fits {
						break
					}
				}
			}
			if fits {
				break
			}
		}
		if compared && !fits {
			return true
		}
	}
	// Counts: each counter both texts have, the bare counts in each.
	var shared []string
	for _, x := range a {
		c := x.num.class
		if x.Kind == NumCount && c != "" && !slices.Contains(shared, c) && slices.ContainsFunc(b, func(y Quantity) bool { return numKeyOf(y) == numKeyOf(x) }) {
			shared = append(shared, c)
		}
	}
	if len(shared) == 0 { // one group, where a bare count meets any counter
		compared := false
		for _, x := range a {
			for _, y := range b {
				if x.Kind == NumCount && numComparable(x, y) {
					if Compatible(x, y) {
						return false
					}
					compared = true
				}
			}
		}
		return compared
	}
	for _, c := range shared {
		in := func(q Quantity) bool { return q.Kind == NumCount && (q.num.class == c || q.num.class == "") }
		fit := false
		for _, x := range a {
			fit = fit || in(x) && slices.ContainsFunc(b, func(y Quantity) bool { return in(y) && Compatible(x, y) })
		}
		if !fit {
			return true
		}
	}
	return false
}

// numMismatchIndex is Mismatch with b's quantities indexed.
func numMismatchIndex(a []Quantity, ib *numIndex) bool {
	fits := map[numKey]bool{} // the groups both texts have: whether a pair fits
	for _, x := range a {
		if x.Kind == NumCount {
			continue
		}
		if k := numKeyOf(x); !fits[k] {
			if st := ib.groups[k]; st != nil {
				fits[k] = st.fits(x)
			}
		}
	}
	for _, ok := range fits {
		if !ok {
			return true
		}
	}
	// Counts: each counter both texts have, the bare counts in each.
	var shared []string
	for _, x := range a {
		if c := x.num.class; x.Kind == NumCount && c != "" && !slices.Contains(shared, c) && ib.groups[numKeyOf(x)] != nil {
			shared = append(shared, c)
		}
	}
	if len(shared) == 0 { // one group, where a bare count meets any counter
		compared := false
		for _, x := range a {
			if st := ib.set(x); x.Kind == NumCount && st != nil {
				if st.fits(x) {
					return false
				}
				compared = true
			}
		}
		return compared
	}
	for _, c := range shared {
		st, fit := ib.groups[numKey{NumCount, c}], false
		for _, x := range a {
			if x.Kind == NumCount && (x.num.class == c || x.num.class == "") && st.fits(x) {
				fit = true
				break
			}
		}
		if !fit {
			return true
		}
	}
	return false
}

// numIndex holds a text's quantities so that whether one of them fits a
// quantity takes a binary search, not a scan.
type numIndex struct {
	groups map[numKey]*numSet // a counter's holds the bare counts too
	counts *numSet            // every count, for a bare one
	bare   *numSet            // the bare counts, for a counter the text does not have
}

// numIndexOf indexes qs.
func numIndexOf(qs []Quantity) *numIndex {
	ix := &numIndex{groups: map[numKey]*numSet{}}
	lists := map[numKey][]Quantity{}
	var counts, bare []Quantity
	for _, q := range qs {
		if q.Kind == NumCount {
			counts = append(counts, q)
			if q.num.class == "" {
				bare = append(bare, q)
				continue
			}
		}
		lists[numKeyOf(q)] = append(lists[numKeyOf(q)], q)
	}
	for k, l := range lists {
		if k.kind == NumCount {
			l = append(l[:len(l):len(l)], bare...)
		}
		ix.groups[k] = numNewSet(l)
	}
	if len(counts) > 0 {
		ix.counts = numNewSet(counts)
	}
	if len(bare) > 0 {
		ix.bare = numNewSet(bare)
	}
	return ix
}

// set is the quantities x can be compared with (numComparable), or nil.
func (ix *numIndex) set(x Quantity) *numSet {
	switch {
	case x.Kind != NumCount:
		return ix.groups[numKeyOf(x)]
	case x.num.class == "":
		return ix.counts
	}
	if st := ix.groups[numKeyOf(x)]; st != nil {
		return st
	}
	return ix.bare
}

// numSet is quantities of one kind and class, set apart by shape so that
// Compatible with one of them needs no scan, but for limits given as
// ranges and for versions, which are few.
type numSet struct {
	shapes [2]numShapes // [0] not a date or a date with a year, [1] a date without one
	// A date without a year and one with a year are compared by days of
	// the year, whatever their shapes.
	daysDated, daysYearless numSpans
	versions                []numValue
}

// numShapes are the values of one set, and of one yearlessness, by shape.
type numShapes struct {
	points             numSpans   // by [lo, hi]
	minNomHi, maxNomLo float64    // of the points: whether one satisfies a limit
	ranges             []numValue // the points that are ranges, for a limit given as a range
	upMax, lowMin      float64    // the highest hi of the upper limits, the lowest lo of the lower ones
	up, low            numSpans   // the limits by [lo, hi], for another limit
	rangeLimits        []numValue
}

// numSpans are intervals sorted by lo, with the highest hi up to each.
type numSpans struct{ lo, hiMax []float64 }

func numNewSpans(spans [][2]float64) numSpans {
	slices.SortFunc(spans, func(p, q [2]float64) int { return cmp.Compare(p[0], q[0]) })
	sp := numSpans{lo: make([]float64, len(spans)), hiMax: make([]float64, len(spans))}
	for i, x := range spans {
		sp.lo[i], sp.hiMax[i] = x[0], x[1]
		if i > 0 {
			sp.hiMax[i] = max(x[1], sp.hiMax[i-1])
		}
	}
	return sp
}

// overlaps reports whether one of the intervals shares more than an end
// with [lo, hi] (numOverlap): of those that start before hi, the one that
// ends last ends after lo.
func (sp numSpans) overlaps(lo, hi float64) bool {
	k, _ := slices.BinarySearch(sp.lo, hi) // sp.lo[:k] are below hi
	return k > 0 && sp.hiMax[k-1] > lo
}

func numNewSet(qs []Quantity) *numSet {
	st := &numSet{}
	var points, up, low [2][][2]float64
	var dated, yearless [][2]float64
	for y := range st.shapes {
		sh := &st.shapes[y]
		sh.minNomHi, sh.maxNomLo, sh.upMax, sh.lowMin = math.Inf(1), math.Inf(-1), math.Inf(-1), math.Inf(1)
	}
	for _, q := range qs {
		v := q.num
		if q.Kind == NumVersion {
			st.versions = append(st.versions, v)
			continue
		}
		y := 0
		if q.Kind == NumDate {
			if v.yearless {
				y = 1
				yearless = append(yearless, [2]float64{v.lo, v.hi})
			} else {
				dated = append(dated, [2]float64{v.dayLo, v.dayHi})
			}
		}
		sh := &st.shapes[y]
		switch v.shape {
		case numPoint:
			points[y] = append(points[y], [2]float64{v.lo, v.hi})
			sh.minNomHi, sh.maxNomLo = min(sh.minNomHi, v.nomHi), max(sh.maxNomLo, v.nomLo)
			if v.nomLo < v.nomHi {
				sh.ranges = append(sh.ranges, v)
			}
		case numLimit:
			if v.upper {
				up[y] = append(up[y], [2]float64{v.lo, v.hi})
				sh.upMax = max(sh.upMax, v.hi)
			} else {
				low[y] = append(low[y], [2]float64{v.lo, v.hi})
				sh.lowMin = min(sh.lowMin, v.lo)
			}
		case numRangeLimit:
			sh.rangeLimits = append(sh.rangeLimits, v)
		}
	}
	for y := range st.shapes {
		st.shapes[y].points, st.shapes[y].up, st.shapes[y].low = numNewSpans(points[y]), numNewSpans(up[y]), numNewSpans(low[y])
	}
	st.daysDated, st.daysYearless = numNewSpans(dated), numNewSpans(yearless)
	return st
}

// fits reports whether a quantity of the set is Compatible with x, which
// is comparable with them all.
func (st *numSet) fits(x Quantity) bool {
	v := x.num
	if x.Kind == NumVersion {
		return slices.ContainsFunc(st.versions, func(y numValue) bool { return numVersionFits(v, y) })
	}
	y := 0
	if x.Kind == NumDate {
		if v.yearless {
			if st.daysDated.overlaps(v.lo, v.hi) {
				return true
			}
			y = 1
		} else if st.daysYearless.overlaps(v.dayLo, v.dayHi) {
			return true
		}
	}
	sh := &st.shapes[y]
	switch v.shape {
	case numPoint:
		// a point, a limit it satisfies, or a limit given as its range
		return sh.points.overlaps(v.lo, v.hi) || v.nomHi < sh.upMax || v.nomLo > sh.lowMin ||
			v.nomLo < v.nomHi && slices.ContainsFunc(sh.rangeLimits, func(r numValue) bool { return numSameRange(v, r) })
	case numLimit:
		// a limit of its direction and value, or a point that satisfies it
		if v.upper {
			return sh.up.overlaps(v.lo, v.hi) || sh.minNomHi < v.hi
		}
		return sh.low.overlaps(v.lo, v.hi) || sh.maxNomLo > v.lo
	case numRangeLimit:
		return slices.ContainsFunc(sh.ranges, func(p numValue) bool { return numSameRange(p, v) }) ||
			slices.ContainsFunc(sh.rangeLimits, func(r numValue) bool { return r.upper == v.upper && numSameRange(v, r) })
	}
	return false
}
