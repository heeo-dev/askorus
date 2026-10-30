// Inline math in answers ($…$ and \(…\)) is shown as plain Unicode text, so
// it reads inside a Korean or English sentence without a WebView: "\pi r^2"
// becomes "πr²". The TeX is parsed (tokens, then groups and arguments), not
// patched with regexes, and anything that cannot be shown faithfully, such as
// a matrix or an unknown command, gives null so the caller shows the source
// in code style. Display math ($$…$$) is drawn with KaTeX (MathBlock.tsx).

const maxLength = 1000;
const maxDepth = 20; // nested groups and arguments; real answers use three or four

const words = (s: string) => s.trim().split(/\s+/);

// pairs reads "name shown name shown …" into a map.
function pairs(s: string): Map<string, string> {
  const w = words(s);
  const m = new Map<string, string>();
  for (let i = 0; i + 1 < w.length; i += 2) m.set(w[i], w[i + 1]);
  return m;
}

// Letters and letter-like symbols. A space that only ends their command is
// dropped, as in TeX: "\pi r" is πr.
const letters = pairs(`
  alpha α beta β gamma γ delta δ epsilon ϵ varepsilon ε zeta ζ eta η theta θ vartheta ϑ iota ι kappa κ lambda λ
  mu μ nu ν xi ξ omicron ο pi π varpi ϖ rho ρ varrho ϱ sigma σ varsigma ς tau τ upsilon υ phi ϕ varphi φ chi χ
  psi ψ omega ω Gamma Γ Delta Δ Theta Θ Lambda Λ Xi Ξ Pi Π Sigma Σ Upsilon Υ Phi Φ Psi Ψ Omega Ω
  infty ∞ partial ∂ nabla ∇ hbar ℏ ell ℓ Re ℜ Im ℑ aleph ℵ emptyset ∅ varnothing ∅ forall ∀ exists ∃ nexists ∄
  neg ¬ lnot ¬ prime ′ degree ° angle ∠ triangle △ checkmark ✓
`);

// Operators, relations, arrows, dots, brackets and escaped characters. The
// author's spaces around them stay, the same on both sides: "a \cdot b" is
// a · b and "a\cdot b" is a·b, never a· b.
const symbols = pairs(`
  pm ± mp ∓ times × div ÷ cdot · ast ∗ star ⋆ circ ∘ bullet • oplus ⊕ otimes ⊗
  leq ≤ le ≤ geq ≥ ge ≥ neq ≠ ne ≠ approx ≈ equiv ≡ sim ∼ simeq ≃ cong ≅ propto ∝ ll ≪ gg ≫ lt < gt >
  subset ⊂ subseteq ⊆ supset ⊃ supseteq ⊇ in ∈ notin ∉ ni ∋ cup ∪ cap ∩ setminus ∖ land ∧ wedge ∧ lor ∨ vee ∨
  to → rightarrow → leftarrow ← gets ← Rightarrow ⇒ implies ⇒ Leftarrow ⇐ Leftrightarrow ⇔ iff ⇔ leftrightarrow ↔
  mapsto ↦ longrightarrow ⟶ Longrightarrow ⟹ longleftarrow ⟵ Longleftarrow ⟸ uparrow ↑ downarrow ↓
  perp ⊥ parallel ∥ mid ∣ colon : ldots … dots … cdots ⋯ vdots ⋮ ddots ⋱ therefore ∴ because ∵ dagger † ddagger ‡
  langle ⟨ rangle ⟩ lfloor ⌊ rfloor ⌋ lceil ⌈ rceil ⌉ vert | Vert ‖ lvert | rvert | lVert ‖ rVert ‖ lbrace { rbrace }
  | ‖ { { } } % % $ $ & & # # _ _
`);

const bigOps = pairs('sum ∑ prod ∏ coprod ∐ int ∫ iint ∬ iiint ∭ oint ∮');
const opNames = new Set(
  words(`sin cos tan cot sec csc arcsin arccos arctan sinh cosh tanh coth log ln lg exp lim limsup liminf
    max min sup inf det dim ker deg gcd arg Pr mod`),
);
const doubleStruck = pairs('N ℕ Z ℤ Q ℚ R ℝ C ℂ P ℙ H ℍ');
// combining marks, as escapes: they would sit on the character before them
const accents = new Map([
  ['bar', '\u0304'],
  ['overline', '\u0305'],
  ['hat', '\u0302'],
  ['widehat', '\u0302'],
  ['tilde', '\u0303'],
  ['widetilde', '\u0303'],
  ['vec', '\u20d7'],
  ['dot', '\u0307'],
  ['ddot', '\u0308'],
]);
const roots = new Map([['', '√'], ['2', '√'], ['3', '∛'], ['4', '∜']]);

const gaps = new Set([' ', ...words(', : ; > quad qquad enspace thinspace medspace thickspace')]);
// style, size and negative-space commands: nothing to show
const dropped = new Set(
  words('displaystyle textstyle scriptstyle scriptscriptstyle limits nolimits ! negthinspace negmedspace negthickspace'),
);
const bigs = new Set(words('big Big bigg Bigg bigl bigr bigm Bigl Bigr Bigm biggl biggr biggm Biggl Biggr Biggm'));
const textFonts = new Set(words('text textrm textit textbf textsf texttt textup textnormal mbox'));
const mathFonts = new Set(words('mathrm mathit mathbf mathsf mathtt mathnormal mathcal boldsymbol bm'));
// Font and text content that reads as one word (CO, m/s, 넓이) takes a script
// straight after it, as TeX shows it: \mathrm{CO}_2 is CO₂, \mathrm{m/s}^2 is
// m/s². Spaces or operators make it several things: (a + b)².
const oneWord = (s: string) => s !== '' && !/[\s+\-−=<>,;±∓×÷]/.test(s);

// Delimiters after \left, \right, \middle and \big: "." is the empty one.
const delimChars = pairs('( ( ) ) [ [ ] ] | | / / < ⟨ > ⟩');
const delimCmds = new Set(
  words('{ } | langle rangle lfloor rfloor lceil rceil vert Vert lvert rvert lVert rVert lbrace rbrace uparrow downarrow'),
);
const opening = new Set(['(', '[', '{', '⟨', '⌊', '⌈']);
const closing = new Set([')', ']', '}', '⟩', '⌋', '⌉']);

function scriptMap(from: string, to: string): Map<string, string> {
  const t = Array.from(to);
  return new Map(Array.from(from).map((ch, i) => [ch, t[i]]));
}
const supMap = scriptMap(
  '0123456789+−-=()abcdefghijklmnoprstuvwxyzABDEGHIJKLMNOPRTUVW',
  '⁰¹²³⁴⁵⁶⁷⁸⁹⁺⁻⁻⁼⁽⁾ᵃᵇᶜᵈᵉᶠᵍʰⁱʲᵏˡᵐⁿᵒᵖʳˢᵗᵘᵛʷˣʸᶻᴬᴮᴰᴱᴳᴴᴵᴶᴷᴸᴹᴺᴼᴾᴿᵀᵁⱽᵂ',
);
const subMap = scriptMap('0123456789+−-=()aehijklmnoprstuvx', '₀₁₂₃₄₅₆₇₈₉₊₋₋₌₍₎ₐₑₕᵢⱼₖₗₘₙₒₚᵣₛₜᵤᵥₓ');
const primes = ['′', '″', '‴', '⁗'];
const scriptChars = new Set([...supMap.values(), ...subMap.values(), ...primes, '°']);

// Tokens: 'cmd' is a control word or symbol without its backslash, 'ch' one
// character, 'sp' a run of spaces (v is 'end' right after a control word,
// where TeX takes it as only ending the word, as in "\pi r"), and the marks
// { } ^ _.
type Tok = { k: 'cmd' | 'ch' | 'sp' | '{' | '}' | '^' | '_'; v: string };

// An atom is one thing in a row of math (a character, symbol, number,
// operator name or built thing like a fraction) with its scripts.
type Atom = {
  text: string;
  unit: boolean; // reads as one thing, so "/", "√" and scripts need no brackets around it
  letter?: boolean; // from a letter-like command (\pi)
  op?: boolean; // an operator name or big operator: spaced from a letter or digit, like TeX
  frac?: boolean; // "a/b": bracketed when a factor follows closely, "(1/2)x"
  root?: boolean; // "√2": a factor right after it would read as under it, "√2x"
  num?: boolean; // digits, which the next digit or decimal point joins
  open?: boolean; // an opening bracket: no space after it
  close?: boolean; // a closing bracket: no space before it
  sub?: string;
  sup?: string;
};
type Item = Atom | 'gap'; // 'gap' is a space the author wrote

const atom = (text: string, more?: Partial<Atom>): Atom => ({
  text,
  unit: true,
  open: opening.has(text),
  close: closing.has(text),
  ...more,
});
const show = (a: Atom) => a.text + (a.sub ?? '') + (a.sup ?? '');
const scripted = (a: Atom) => a.sub !== undefined || a.sup !== undefined;

// Giving up unwinds the whole parse; texToUnicode turns it into null.
const giveUp = Symbol('tex');
const stop = (): never => {
  throw giveUp;
};

/**
 * texToUnicode turns one inline TeX expression (without its $ or \( \)
 * marks) into plain Unicode text, or null when it cannot be shown
 * faithfully. It never throws.
 */
export function texToUnicode(tex: string): string | null {
  if (typeof tex !== 'string' || tex.length > maxLength) return null;
  try {
    const toks = tokenize(tex);
    if (!toks) return null;
    const p = new Parser(toks);
    const out = p.join(p.row('end')).replace(/ {2,}/g, ' ').trim();
    return out || null;
  } catch {
    return null; // giveUp, or anything unexpected: the caller shows the source
  }
}

const isSpace = (ch: string | undefined) => ch === ' ' || ch === '\t' || ch === '\n' || ch === '\r';
const isLetter = (ch: string | undefined) => ch !== undefined && ((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z'));
const isDigit = (ch: string | undefined) => ch !== undefined && ch.length === 1 && ch >= '0' && ch <= '9';

function tokenize(s: string): Tok[] | null {
  const toks: Tok[] = [];
  let i = 0;
  while (i < s.length) {
    const ch = String.fromCodePoint(s.codePointAt(i) ?? 0);
    i += ch.length;
    if (ch === '\\') {
      let j = i;
      while (isLetter(s[j])) j++;
      if (j > i) {
        toks.push({ k: 'cmd', v: s.slice(i, j) });
        let k = j;
        while (isSpace(s[k])) k++;
        if (k > j) toks.push({ k: 'sp', v: 'end' });
        i = k;
      } else {
        if (i >= s.length) return null; // a backslash at the very end
        const sym = String.fromCodePoint(s.codePointAt(i) ?? 0);
        toks.push({ k: 'cmd', v: isSpace(sym) ? ' ' : sym });
        i += sym.length;
      }
    } else if (isSpace(ch)) {
      if (toks[toks.length - 1]?.k !== 'sp') toks.push({ k: 'sp', v: '' });
    } else if (ch === '&' || ch === '$' || ch === '#') {
      return null; // a table cell, a stray math mark or a macro parameter
    } else {
      toks.push({ k: ch === '{' || ch === '}' || ch === '^' || ch === '_' ? ch : 'ch', v: ch });
    }
  }
  return toks;
}

class Parser {
  private readonly toks: Tok[];
  private i = 0;
  private depth = 0; // groups and arguments open
  private script = 0; // scripts open: spaces vanish there, and a script inside one must fit Unicode
  private fence = 0; // \left … \right open

  constructor(toks: Tok[]) {
    this.toks = toks;
  }

  /** row reads items up to the end, a '}', a ']' or a \right (which it leaves for \left). */
  row(until: 'end' | '}' | ']' | 'right', text = false): Item[] {
    const items: Item[] = [];
    for (;;) {
      const t = this.toks[this.i];
      if (!t) return until === 'end' ? items : stop();
      if (t.k === '}') {
        if (until !== '}') stop();
        this.i++;
        return items;
      }
      if (until === ']' && t.k === 'ch' && t.v === ']') {
        this.i++;
        return items;
      }
      if (until === 'right' && t.k === 'cmd' && t.v === 'right') return items;
      if (t.k === 'sp') {
        this.i++;
        if (t.v === 'end' && !this.keepEnd(items, text)) continue;
        items.push(text ? atom(' ') : 'gap');
      } else if (t.k === '^' || t.k === '_') {
        if (text) stop();
        this.i++;
        this.attach(items, t.k === '^' ? 'sup' : 'sub');
      } else {
        this.one(items, text);
      }
    }
  }

  /** join writes a row: one space for the author's spaces (none in scripts) and around operator names. */
  join(items: Item[]): string {
    const bars = pairBars(items);
    let out = '';
    let gap = false;
    let prev: Atom | undefined;
    let prevOpen = false;
    items.forEach((it, k) => {
      if (it === 'gap') {
        gap = true;
        return;
      }
      let s = show(it);
      if (!s) return;
      const open = it.open || bars.get(k) === 'open';
      const close = it.close || bars.get(k) === 'close';
      const kept = out !== '' && gap && !this.script && !prevOpen && !close;
      let glue = ''; // between two things that would otherwise run together
      if (it.frac) {
        let n = k + 1;
        while (items[n] === 'gap') n++;
        const next = items[n];
        const after = next !== undefined && next !== 'gap' ? show(next) : '';
        const factor = next !== undefined && next !== 'gap' && (next.op || opensFactor(after) || /^[!′″‴%]/.test(after));
        const name = prev?.op && isWord(prev.text.codePointAt(0)); // sin, not ∑
        const number = prev?.num === true && !scripted(prev);
        const term = !kept && prev !== undefined && !prev.op && !number && endsTerm(out) && !s.startsWith('(');
        // "1/2 x" would read as 1/(2x), "1/2!" as 1/(2!), "sin π/2" as sin(π)/2, "a/b/c" as
        // (a/b)/c, "a ÷ b/c" as (a ÷ b)/c, and "x1/2" or "√31/2" as one name or root
        if (factor || name || term || /[/÷]$/.test(out)) s = `(${s})`;
        // a number and a fraction make a mixed number, written "2 1/3" in plain text
        if (number && !kept && isDigit(it.text.charAt(0))) glue = ' ';
      }
      // "√2x" would read as √(2x)
      if (prev?.root && !scripted(prev) && !it.op && isWord(s.codePointAt(0))) glue = '·';
      if (out) {
        const op = (prev?.op && isWord(s.codePointAt(0))) || (it.op && endsTerm(out));
        out += kept || op ? ' ' : glue;
      }
      out += s;
      gap = false;
      prev = it;
      prevOpen = open;
    });
    return out;
  }

  /**
   * keepEnd tells whether a space right after a control word stays. TeX takes it
   * as only ending the word; here it stays where it reads as the author's spacing:
   * around a symbol spaced on its other side too ("a \cdot b" is a · b, "a\cdot b"
   * is a·b), and after a letter or operator name unless a factor follows ("\alpha = 1"
   * is α = 1, but "\pi r" is πr, "\pi 2" is π2 and "\sin (x)" is sin(x)).
   */
  private keepEnd(items: Item[], text: boolean): boolean {
    const last = items[items.length - 1];
    const cmd = this.toks[this.i - 2]?.v ?? '';
    if (text || last === undefined || last === 'gap') return false;
    if (scripted(last) || bigOps.has(cmd)) return true; // "30^\circ C", "\int f"
    if (symbols.has(cmd)) return items.length < 2 || items[items.length - 2] === 'gap';
    if (letters.has(cmd) || opNames.has(cmd)) return !this.factorNext();
    return false;
  }

  /** factorNext tells whether the next token starts a factor that a letter or operator name runs into. */
  private factorNext(): boolean {
    const t = this.toks[this.i];
    if (t?.k === 'ch') return isWord(t.v.codePointAt(0)) || t.v === '(' || t.v === '[';
    return t?.k === 'cmd' && (letters.has(t.v) || t.v === 'left');
  }

  private one(items: Item[], text: boolean): void {
    const t = this.toks[this.i++];
    if (t?.k === '{') {
      const inner = this.nest(() => this.row('}', text));
      const s = this.join(inner);
      items.push(atom(s, { unit: simple(inner, s, true), open: false, close: false }));
    } else if (t?.k === 'ch') {
      this.char(items, t.v, text);
    } else if (t?.k === 'cmd') {
      this.command(items, t.v, text);
    } else {
      stop(); // '}', '^' or '_' where something was expected, or the end
    }
  }

  private char(items: Item[], ch: string, text: boolean): void {
    if (text) {
      items.push(atom(ch === '~' ? ' ' : ch));
      return;
    }
    const last = items[items.length - 1];
    const bare = last !== undefined && last !== 'gap' && !scripted(last) ? last : undefined;
    const next = this.toks[this.i];
    if (isDigit(ch) || (ch === '.' && bare?.num && next?.k === 'ch' && isDigit(next.v))) {
      if (bare?.num) bare.text += ch;
      else items.push(atom(ch, { num: true }));
    } else if (ch === "'") {
      const n = bare ? primes.indexOf(bare.text) : -1;
      if (bare && n >= 0 && n < primes.length - 1) bare.text = primes[n + 1];
      else items.push(atom('′'));
    } else if (ch === '~') {
      items.push('gap');
    } else {
      items.push(atom(ch === '-' ? '−' : ch === '*' ? '∗' : ch));
    }
  }

  private command(items: Item[], name: string, text: boolean): void {
    if (gaps.has(name)) {
      items.push(text ? atom(' ') : 'gap');
      return;
    }
    if (dropped.has(name)) return;
    const sym = letters.get(name) ?? symbols.get(name);
    if (sym !== undefined) {
      const a = atom(sym, { letter: letters.has(name) });
      if (name === 'lvert' || name === 'lVert') a.open = true; // "|" alone could open or close
      if (name === 'rvert' || name === 'rVert') a.close = true;
      items.push(a);
      return;
    }
    if (textFonts.has(name)) {
      const s = this.join(this.arg(true));
      items.push(atom(s, { unit: oneWord(s), open: false, close: false }));
      return;
    }
    if (text) stop(); // the rest is math only
    const big = bigOps.get(name);
    if (big !== undefined) {
      items.push(atom(big, { op: true }));
      return;
    }
    if (opNames.has(name)) {
      items.push(atom(name === 'limsup' ? 'lim sup' : name === 'liminf' ? 'lim inf' : name, { op: true }));
      return;
    }
    if (mathFonts.has(name)) {
      const inner = this.arg(false);
      const s = this.join(inner);
      items.push(atom(s, { unit: simple(inner, s, true) || oneWord(s), open: false, close: false }));
      return;
    }
    const mark = accents.get(name);
    if (mark !== undefined) {
      // The mark goes on each atom of the argument, not on its scripts: in \hat{\beta_1} only the beta gets the hat.
      const arg = this.arg(false);
      const bases = arg.reduce((n, it) => n + (it === 'gap' ? 0 : Array.from(it.text).filter(carries).length), 0);
      const m = name === 'bar' && bases > 1 ? '\u0305' : mark;
      items.push(atom(this.join(arg.map((it) => (it === 'gap' ? it : { ...it, text: accent(it.text, m) })))));
      return;
    }
    if (bigs.has(name)) {
      const d = this.delim();
      if (d) items.push(atom(d));
      return;
    }
    switch (name) {
      case 'frac':
      case 'dfrac':
      case 'tfrac':
      case 'cfrac': {
        const a = this.arg(false);
        const b = this.arg(false);
        items.push(atom(`${this.part(a)}/${this.part(b)}`, { unit: false, frac: true }));
        return;
      }
      case 'binom':
      case 'dbinom':
      case 'tbinom': {
        const a = this.join(this.arg(false));
        const b = this.join(this.arg(false));
        if (!a || !b) stop();
        items.push(atom(`C(${a}, ${b})`));
        return;
      }
      case 'sqrt': {
        this.skipSpace();
        let root = '√';
        const t = this.toks[this.i];
        if (t?.k === 'ch' && t.v === '[') {
          this.i++;
          this.script++; // the index is written small, like a script
          const n = this.join(this.nest(() => this.row(']')));
          this.script--;
          root = roots.get(n) ?? (toScript(n, 'sup') ?? stop()) + '√';
        }
        const body = this.arg(false);
        const s = this.join(body);
        if (!s) stop();
        items.push(atom(root + (simple(body, s, true) ? s : `(${s})`), { root: true }));
        return;
      }
      case 'mathbb': {
        let s = '';
        for (const ch of this.join(this.arg(false))) if (ch !== ' ') s += doubleStruck.get(ch) ?? stop();
        items.push(atom(s));
        return;
      }
      case 'operatorname': {
        const t = this.toks[this.i];
        if (t?.k === 'ch' && t.v === '*') this.i++;
        items.push(atom(this.join(this.arg(false)), { op: true }));
        return;
      }
      case 'left': {
        const open = this.delim();
        this.fence++;
        const inner = this.nest(() => this.row('right'));
        this.fence--;
        this.i++; // \right
        items.push(atom(open + this.join(inner) + this.delim(), { open: false, close: false }));
        return;
      }
      case 'middle':
        if (!this.fence) stop();
        items.push(atom(this.delim()));
        return;
      case 'bmod':
        // an operator name too, so it keeps its spaces in a script: x^(a mod b)
        items.push('gap', atom('mod', { op: true }), 'gap');
        return;
      case 'pmod':
        items.push('gap', atom(`(mod ${this.join(this.arg(false))})`, { open: false, close: false }));
        return;
      default:
        stop(); // \begin, \\, \over, \right without \left, and anything unknown
    }
  }

  /** attach puts a superscript or subscript on the atom before it. */
  private attach(items: Item[], key: 'sup' | 'sub'): void {
    let base = items[items.length - 1];
    while (base === 'gap') {
      items.pop(); // "x ^2" is x² in TeX
      base = items[items.length - 1];
    }
    if (base === undefined) {
      base = atom(''); // "^2" with nothing before it
      items.push(base);
    }
    if (base[key] !== undefined) stop(); // x^a^b is an error in TeX too
    this.script++;
    const s = this.join(this.arg(false));
    this.script--;
    // Inside a script a nested one must fit Unicode: "e^(x^(iπ))" is too much.
    const shown = toScript(s, key) ?? (this.script > 0 ? stop() : fallback(s, key));
    // {a+b}^2 is (a+b)², not a+b², and \sqrt{x}^2 is (√x)², not √x², which reads as √(x²)
    if (!base.unit || base.root) base.text = `(${base.text})`;
    base.unit = true;
    base.frac = false;
    base.root = false;
    base[key] = shown;
  }

  /** arg reads one argument: a {group} or a single token, as in \frac12. */
  private arg(text: boolean): Item[] {
    this.skipSpace();
    if (this.toks[this.i]?.k === '{') {
      this.i++;
      return this.nest(() => this.row('}', text));
    }
    const items: Item[] = [];
    this.nest(() => this.one(items, text));
    return items;
  }

  /** delim reads the delimiter after \left, \right, \middle or \big. */
  private delim(): string {
    this.skipSpace();
    const t = this.toks[this.i++];
    if (t?.k === 'ch' && t.v === '.') return '';
    const d = t?.k === 'ch' ? delimChars.get(t.v) : t?.k === 'cmd' && delimCmds.has(t.v) ? symbols.get(t.v) : undefined;
    return d ?? stop();
  }

  /** part writes one side of a fraction, in brackets unless it is simple. */
  private part(items: Item[]): string {
    const s = this.join(items);
    if (!s) stop(); // \frac{}{2} shows nothing faithful
    return simple(items, s) ? s : `(${s})`;
  }

  private skipSpace(): void {
    while (this.toks[this.i]?.k === 'sp') this.i++;
  }

  private nest<T>(read: () => T): T {
    if (++this.depth > maxDepth) stop();
    const r = read();
    this.depth--;
    return r;
  }
}

// simple tells whether a fraction side or a root reads as one thing without
// brackets: one character or symbol, a number, one atom with scripts (x²), or
// something already in one pair of brackets. Under a root (root true) an atom
// with scripts is not simple, since √x² could be (√x)².
function simple(items: Item[], text: string, root = false): boolean {
  const atoms = items.filter((it): it is Atom => it !== 'gap');
  const one = atoms.length === 1 ? atoms[0] : undefined;
  if (one && scripted(one)) return !root;
  // a font's "m/s" is one word to a script, but a fraction or root needs it in brackets: 1/(m/s)
  if (one?.unit && !one.text.includes('/')) return true;
  return atoms.length === 0 || /^(\d+(\.\d*)?|\.\d+)$/.test(text) || wrapped(text, !root);
}

// pairBars finds the bare bars (| and ‖, which can open or close) that pair up
// in a row, as in "| x |", so the spaces inside them go as inside brackets. A
// lone bar, as in "P(A | B)", keeps its spaces, and pairs do not cross brackets.
function pairBars(items: Item[]): Map<number, 'open' | 'close'> {
  const sides = new Map<number, 'open' | 'close'>();
  const frames = [new Map<string, number>()];
  items.forEach((it, k) => {
    if (it === 'gap') return;
    if (it.open) {
      frames.push(new Map());
    } else if (it.close) {
      if (frames.length > 1) frames.pop();
    } else if (it.text === '|' || it.text === '‖') {
      const frame = frames[frames.length - 1];
      const first = frame.get(it.text);
      if (first === undefined) {
        frame.set(it.text, k);
      } else {
        sides.set(first, 'open');
        sides.set(k, 'close');
        frame.delete(it.text);
      }
    }
  });
  return sides;
}

const pairOf = pairs('( ) [ ] { } ⟨ ⟩ ⌊ ⌋ ⌈ ⌉ | | ‖ ‖');

// wrapped tells whether one pair of brackets holds all of s, as in "(a+b)" or
// "|x|", with Unicode scripts allowed after it ("(a+b)²") when scripts is true.
function wrapped(s: string, scripts: boolean): boolean {
  let end = s.length;
  if (scripts) while (end > 0 && scriptChars.has(s[end - 1])) end--;
  const open = s.charAt(0);
  const close = pairOf.get(open);
  if (close === undefined || end < 2 || s[end - 1] !== close) return false;
  if (open === close) return !s.slice(1, end - 1).includes(open);
  let depth = 0;
  for (let i = 0; i < end; i++) {
    if (s[i] === open) depth++;
    else if (s[i] === close && --depth === 0) return i === end - 1;
  }
  return false;
}

// toScript writes s in Unicode superscript or subscript letters, or null when
// one of its characters has no such form.
function toScript(s: string, key: 'sup' | 'sub'): string | null {
  if (key === 'sup' && s === '∘') return '°';
  // there is no raised star; x* and A† are how plain text writes x^* and A^\dagger
  if (key === 'sup' && (s === '∗' || s === '†')) return s === '∗' ? '*' : s;
  if (key === 'sup' && s !== '' && Array.from(s).every((ch) => primes.includes(ch))) return s;
  const map = key === 'sup' ? supMap : subMap;
  let out = '';
  for (const ch of s) {
    const m = map.get(ch);
    if (m === undefined) return null;
    out += m;
  }
  return out;
}

// fallback writes a script that has no Unicode form as "^x" or "^(…)".
function fallback(s: string, key: 'sup' | 'sub'): string {
  const mark = key === 'sup' ? '^' : '_';
  return width(s) === 1 || wrapped(s, false) ? mark + s : `${mark}(${s})`;
}

const isMark = (ch: string | undefined) => {
  const c = ch?.codePointAt(0) ?? 0;
  return (c >= 0x300 && c <= 0x36f) || (c >= 0x20d0 && c <= 0x20ff);
};

// width counts the characters a reader sees: combining marks do not count.
function width(s: string): number {
  let n = 0;
  for (const ch of s) if (!isMark(ch)) n++;
  return n;
}

// carries tells whether an accent's mark goes on ch: not on spaces, marks or scripts.
const carries = (ch: string) => ch !== ' ' && !isMark(ch) && !scriptChars.has(ch);

// accent puts a combining mark over every character of s that carries one.
function accent(s: string, mark: string): string {
  const cs = Array.from(s);
  let out = '';
  let base = false;
  cs.forEach((ch, i) => {
    if (!isMark(ch)) base = carries(ch);
    out += ch;
    if (base && !isMark(cs[i + 1])) out += mark;
  });
  return out;
}

// isWord tells letters and digits from symbols. Hermes may lack \p{L}, so the
// alphabets answers use are listed: Latin, Greek, Cyrillic, Hangul, kana, CJK
// and letter-like symbols such as ℝ and ℏ.
function isWord(c: number | undefined): boolean {
  if (c === undefined) return false;
  return (
    (c >= 0x30 && c <= 0x39) ||
    (c >= 0x41 && c <= 0x5a) ||
    (c >= 0x61 && c <= 0x7a) ||
    (c >= 0xc0 && c <= 0x24f && c !== 0xd7 && c !== 0xf7) ||
    (c >= 0x370 && c <= 0x4ff) ||
    (c >= 0x1100 && c <= 0x11ff) ||
    (c >= 0x2100 && c <= 0x214f) ||
    (c >= 0x3040 && c <= 0x30ff) ||
    (c >= 0x3130 && c <= 0x318f) ||
    (c >= 0x4e00 && c <= 0x9fff) ||
    (c >= 0xac00 && c <= 0xd7a3)
  );
}

// endsTerm tells whether s ends like a factor (x, 2, ), x², \hat{x}), which an
// operator name after it is spaced from: "n log n", not "nlog n". Combining
// marks are looked through, to the letter they sit on.
function endsTerm(s: string): boolean {
  const cs = Array.from(s.slice(-8));
  let n = cs.length - 1;
  while (n > 0 && isMark(cs[n])) n--;
  const last = cs[n] ?? '';
  return isWord(last.codePointAt(0)) || closing.has(last) || scriptChars.has(last);
}

// opensFactor tells whether s starts like a factor, which would run into a
// fraction before it.
const opensFactor = (s: string) => isWord(s.codePointAt(0)) || ['(', '[', '√', '∛', '∜'].includes(s.charAt(0));
