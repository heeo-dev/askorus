// Inline TeX is shown as plain Unicode text ("\pi r^2" is πr²), and what
// cannot be shown faithfully gives null, so the answer shows the source.

import { texToUnicode } from './tex';

const cases = (rows: [string, string | null][]) =>
  test.each(rows)('%s → %s', (tex, out) => {
    expect(texToUnicode(tex)).toBe(out);
  });

// "name shown name shown …", as the symbol rules are written
const pairs = (s: string): [string, string][] => {
  const w = s.trim().split(/\s+/);
  return w.flatMap((name, i): [string, string][] => (i % 2 ? [] : [[name, w[i + 1]]]));
};

describe('real answers', () =>
  cases([
    ['x = \\frac{-b \\pm \\sqrt{b^2-4ac}}{2a}', 'x = (−b ± √(b²−4ac))/(2a)'],
    ['E = mc^2', 'E = mc²'],
    ['\\pi r^2', 'πr²'],
    ['a^2 + b^2 = c^2', 'a² + b² = c²'],
    ['\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2}', '∑ᵢ₌₁ⁿ i = (n(n+1))/2'],
    ['O(n \\log n)', 'O(n log n)'],
    ["f'(x) = 2x", 'f′(x) = 2x'],
    ['\\alpha + \\beta = \\gamma', 'α + β = γ'],
    ['x \\in \\{1, 2, 3\\}', 'x ∈ {1, 2, 3}'],
    ['50\\%', '50%'],
    ['\\sqrt{2} \\approx 1.414', '√2 ≈ 1.414'],
    ['\\Delta x \\to 0', 'Δx → 0'],
    ['P(A \\mid B) = \\frac{P(B \\mid A)P(A)}{P(B)}', 'P(A ∣ B) = (P(B ∣ A)P(A))/(P(B))'],
    ['\\vec{v} = (1, 2)', 'v\u20d7 = (1, 2)'],
    ['\\overline{AB}', 'A\u0305B\u0305'],
    ['\\hat{\\theta}', 'θ\u0302'],
    ['\\sqrt[3]{8} = 2', '∛8 = 2'],
    ['\\binom{n}{k}', 'C(n, k)'],
    ['\\begin{pmatrix}1&2\\\\3&4\\end{pmatrix}', null],
    ['\\unknowncommand', null],
    ['\\frac{1}{', null],
    ['\\text{넓이} = \\pi r^2', '넓이 = πr²'],
    ['넓이 = r^2', '넓이 = r²'],
    ['\\int_0^1 f(x)\\,dx', '∫₀¹ f(x) dx'],
    ['\\lim_{n \\to \\infty} \\frac{1}{n} = 0', 'lim_(n→∞) (1/n) = 0'],
    ['\\sum_{k=0}^{\\infty} \\frac{x^k}{k!}', '∑ₖ₌₀^∞ xᵏ/(k!)'],
    ['\\theta = 30^\\circ', 'θ = 30°'],
    ['\\hat{y} = \\beta_0 + \\beta_1 x', 'y\u0302 = β₀ + β₁ x'],
    ['\\forall x \\in \\mathbb{R}, \\; x^2 \\ge 0', '∀x ∈ ℝ, x² ≥ 0'],
    ['a \\equiv b \\pmod{n}', 'a ≡ b (mod n)'],
    ['H_2O', 'H₂O'],
  ]));

describe('spaces: the author’s stay, one at a time; a space that only ends a command goes', () =>
  cases([
    ['x = 1', 'x = 1'],
    ['x=1', 'x=1'],
    ['x   =    1', 'x = 1'],
    ['  x + y  ', 'x + y'],
    ['2\\pi r', '2πr'],
    ['\\alpha x', 'αx'],
    ['\\alpha\\beta', 'αβ'],
    ['a \\cdot b', 'a · b'],
    ['x \\in A', 'x ∈ A'],
    ['x ^2', 'x²'],
    ['( x + 1 )', '(x + 1)'],
    ['\\langle x, y \\rangle', '⟨x, y⟩'],
    ['\\lvert x \\rvert', '|x|'],
    ['\\{ a_n \\}', '{aₙ}'],
    // around a symbol the space that ends its command stays only when the other side is spaced too
    ['a\\cdot b', 'a·b'],
    ['n\\times n', 'n×n'],
    ['x\\in A', 'x∈A'],
    ['0\\le x\\le 1', '0≤x≤1'],
    ['A\\cup B', 'A∪B'],
    ['p\\Rightarrow q', 'p⇒q'],
    ['x\\to 0', 'x→0'],
    ['2\\times 3', '2×3'],
    ['x = 1\\pm 2', 'x = 1±2'],
    ['\\{x\\mid x>0\\}', '{x∣x>0}'],
    ['a\\equiv b\\pmod{n}', 'a≡b (mod n)'],
    ['\\alpha \\cdot \\beta', 'α · β'],
    ['\\alpha\\cdot \\beta', 'α·β'],
    ['\\Rightarrow x = 2', '⇒ x = 2'],
    // after a letter it goes before a factor, as in TeX, and stays before an operator
    ['\\alpha (t)', 'α(t)'],
    ['\\pi 2', 'π2'],
    ['\\hbar \\omega', 'ℏω'],
    ['\\alpha = 1', 'α = 1'],
    ['\\lambda \\in \\mathbb{R}', 'λ ∈ ℝ'],
    ['\\alpha \\text{and} \\beta', 'α and β'],
    ['30^\\circ C', '30° C'],
    // bare bars that pair up are spaced like brackets; a lone one keeps its spaces
    ['\\| v \\|', '‖v‖'],
    ['\\vert x \\vert', '|x|'],
    ['| x |', '|x|'],
    ['|a| + |b|', '|a| + |b|'],
    ['| x |^2', '|x|²'],
    ['\\{x | x > 0\\}', '{x | x > 0}'],
    ['P(A | B) + P(C | D)', 'P(A | B) + P(C | D)'],
  ]));

describe('minus, primes and asterisk', () =>
  cases([
    ['a - b', 'a − b'],
    ['-1', '−1'],
    ["f'", 'f′'],
    ["f''(x)", 'f″(x)'],
    ["f'''", 'f‴'],
    ['a*b', 'a∗b'],
  ]));

describe('Greek letters', () =>
  cases([
    ['\\alpha\\beta\\gamma\\delta\\epsilon\\zeta\\eta\\theta\\iota\\kappa\\lambda\\mu', 'αβγδϵζηθικλμ'],
    ['\\nu\\xi\\omicron\\pi\\rho\\sigma\\tau\\upsilon\\phi\\chi\\psi\\omega', 'νξοπρστυϕχψω'],
    ['\\varepsilon\\vartheta\\varphi\\varrho\\varsigma\\varpi', 'εϑφϱςϖ'],
    ['\\Gamma\\Delta\\Theta\\Lambda\\Xi\\Pi\\Sigma\\Upsilon\\Phi\\Psi\\Omega', 'ΓΔΘΛΞΠΣΥΦΨΩ'],
  ]));

describe('relations, operators, arrows and other symbols', () => {
  const symbols = pairs(`
    pm ± mp ∓ times × div ÷ cdot · ast ∗ star ⋆ circ ∘ bullet • oplus ⊕ otimes ⊗ leq ≤ le ≤ geq ≥ ge ≥ neq ≠ ne ≠
    approx ≈ equiv ≡ sim ∼ simeq ≃ cong ≅ propto ∝ ll ≪ gg ≫ subset ⊂ subseteq ⊆ supset ⊃ supseteq ⊇ in ∈ notin ∉
    ni ∋ cup ∪ cap ∩ setminus ∖ emptyset ∅ varnothing ∅ forall ∀ exists ∃ nexists ∄ neg ¬ lnot ¬ land ∧ wedge ∧
    lor ∨ vee ∨ to → rightarrow → leftarrow ← gets ← Rightarrow ⇒ implies ⇒ Leftarrow ⇐ Leftrightarrow ⇔ iff ⇔
    leftrightarrow ↔ mapsto ↦ longrightarrow ⟶ uparrow ↑ downarrow ↓ infty ∞ partial ∂ nabla ∇ sum ∑ prod ∏
    coprod ∐ int ∫ iint ∬ iiint ∭ oint ∮ angle ∠ triangle △ perp ⊥ parallel ∥ mid ∣ hbar ℏ ell ℓ Re ℜ Im ℑ
    aleph ℵ prime ′ degree ° ldots … dots … cdots ⋯ vdots ⋮ ddots ⋱ therefore ∴ because ∵ langle ⟨ rangle ⟩
    lfloor ⌊ rfloor ⌋ lceil ⌈ rceil ⌉ | ‖ vert | Vert ‖ { { } } % % $ $ & & # # _ _ checkmark ✓
  `);
  test.each(symbols)('\\%s → %s', (name, shown) => {
    expect(texToUnicode(`\\${name}`)).toBe(shown);
  });
});

describe('operator names', () => {
  const names = 'sin cos tan cot sec csc arcsin arccos arctan sinh cosh tanh log ln lg exp lim max min sup inf det dim ker deg gcd arg Pr';
  test.each([...names.split(' '), 'mod'])('\\%s is a word', (name) => {
    expect(texToUnicode(`\\${name}`)).toBe(name);
  });
  cases([
    ['\\limsup', 'lim sup'],
    ['\\liminf', 'lim inf'],
    ['\\sin x', 'sin x'],
    ['\\sin(x)', 'sin(x)'],
    ['\\sin (x)', 'sin(x)'],
    ['\\log (n)', 'log(n)'],
    ['\\sin \\left( x \\right)', 'sin(x)'],
    ['\\max = 5', 'max = 5'],
    ['\\hat{x}\\sin x', 'x\u0302 sin x'],
    ['\\log_2 n', 'log₂ n'],
    ['\\log_2n', 'log₂ n'],
    ['\\sin\\theta', 'sin θ'],
    ['\\cos^2\\theta + \\sin^2\\theta = 1', 'cos² θ + sin² θ = 1'],
    ['2\\sin x', '2 sin x'],
    ['\\max_{i} a_i', 'maxᵢ aᵢ'],
    ['\\lim_{x \\to 0}', 'lim_(x→0)'],
    ['\\operatorname{sgn} x', 'sgn x'],
    ['\\operatorname{sgn}(x)', 'sgn(x)'],
    ['\\operatorname*{argmax}_x', 'argmaxₓ'],
    ['a \\bmod b', 'a mod b'],
    ['a\\bmod b', 'a mod b'],
    ['x^{a \\bmod b}', 'x^(a mod b)'],
    ['a \\mod n', 'a mod n'],
    ['\\pmod{n}', '(mod n)'],
  ]);
});

describe('fonts and text', () =>
  cases([
    ['\\text{if } x > 0', 'if x > 0'],
    ['x\\text{ if }y', 'x if y'],
    ['\\text{x-1}', 'x-1'],
    ['\\text{50\\% off}', '50% off'],
    ['\\textrm{rad}', 'rad'],
    ['\\textit{word}', 'word'],
    ['\\textbf{bold}', 'bold'],
    ['\\textnormal{n}', 'n'],
    ['\\mbox{a b}', 'a b'],
    ['\\mathrm{d}x', 'dx'],
    ['\\mathrm{m/s}', 'm/s'],
    ['\\mathit{ab}', 'ab'],
    ['\\mathbf{v} \\cdot \\mathbf{w}', 'v · w'],
    ['\\mathsf{A}', 'A'],
    ['\\mathtt{x}', 'x'],
    ['\\boldsymbol{\\alpha}', 'α'],
    ['\\bm{x}', 'x'],
    ['\\mathcal{L}', 'L'],
    ['\\mathbb{N}\\mathbb{Z}\\mathbb{Q}\\mathbb{R}\\mathbb{C}\\mathbb{P}\\mathbb{H}', 'ℕℤℚℝℂℙℍ'],
    ['\\mathbb R', 'ℝ'],
    ['\\mathbb{A}', null],
    ['\\mathbb{1}', null],
    ['\\text{a^b}', null],
    // a word in a font takes its script straight after it, as TeX shows it
    ['\\mathrm{CO}_2', 'CO₂'],
    ['\\mathrm{Fe}_2\\mathrm{O}_3', 'Fe₂O₃'],
    ['\\mathrm{SO}_4^{2-}', 'SO₄²⁻'],
    ['\\mathrm{Na}^+', 'Na⁺'],
    ['\\mathrm{Ca}^{2+}', 'Ca²⁺'],
    ['\\mathrm{m/s}^2', 'm/s²'],
    ['\\mathrm{cm}^2', 'cm²'],
    ['\\mathrm{mol}^{-1}', 'mol⁻¹'],
    ['\\mathbf{AB}^T', 'ABᵀ'],
    ['\\mathrm{Var}^2', 'Var²'],
    ['\\text{CO}_2', 'CO₂'],
    ['\\frac{\\mathrm{km}}{\\mathrm{h}}', 'km/h'],
    ['\\frac{1}{\\mathrm{m/s}}', '1/(m/s)'],
    ['\\sqrt{\\mathrm{m/s}}', '√(m/s)'],
    ['\\mathbf{a+b}^2', '(a+b)²'],
    ['\\text{x-1}^2', '(x-1)²'],
  ]));

describe('style and size commands go, their delimiters stay', () =>
  cases([
    ['\\displaystyle \\frac{1}{2}', '1/2'],
    ['\\textstyle x \\scriptstyle y', 'x y'],
    ['\\sum\\limits_{i=1}^n i', '∑ᵢ₌₁ⁿ i'],
    ['\\int\\nolimits_0^1', '∫₀¹'],
    ['\\left( x \\right)', '(x)'],
    ['\\left( \\frac{a}{b} \\right)^2', '(a/b)²'],
    ['\\left. \\frac{df}{dx} \\right|_{x=0}', '(df)/(dx)|ₓ₌₀'],
    ['\\left\\{ x \\middle| x > 0 \\right\\}', '{x | x > 0}'],
    ['\\left\\langle v \\right\\rangle', '⟨v⟩'],
    ['\\left[ 0, 1 \\right.', '[0, 1'],
    ['\\big( x \\big)', '(x)'],
    ['\\Bigl[ x \\Bigr]', '[x]'],
    ['\\bigg|x\\bigg|', '|x|'],
    ['\\Biggl\\lfloor x \\Biggr\\rfloor', '⌊x⌋'],
    ['\\left( x', null],
    ['x \\right)', null],
    ['\\middle|', null],
    ['\\left x \\right)', null],
  ]));

describe('spacing commands', () =>
  cases([
    ['a\\,b', 'a b'],
    ['a\\:b', 'a b'],
    ['a\\;b', 'a b'],
    ['a\\>b', 'a b'],
    ['a\\ b', 'a b'],
    ['a\\quad b', 'a b'],
    ['a\\qquad b', 'a b'],
    ['a~b', 'a b'],
    ['a \\quad \\quad b', 'a b'],
    ['a\\!b', 'ab'],
  ]));

describe('accents', () =>
  cases([
    ['\\bar{x}', 'x\u0304'],
    ['\\bar{xy}', 'x\u0305y\u0305'],
    ['\\overline{x}', 'x\u0305'],
    ['\\hat{x}', 'x\u0302'],
    ['\\widehat{AB}', 'A\u0302B\u0302'],
    ['\\tilde{n}', 'n\u0303'],
    ['\\widetilde{xy}', 'x\u0303y\u0303'],
    ['\\vec{AB}', 'A\u20d7B\u20d7'],
    ['\\dot{x}', 'x\u0307'],
    ['\\ddot{x}', 'x\u0308'],
    ['\\hat{x}^2', 'x\u0302²'],
    // the mark sits on the letter, not on a script inside the argument
    ['\\hat{\\beta_1}', 'β\u0302₁'],
    ['\\hat{x_1}', 'x\u0302₁'],
    ['\\hat{x^2}', 'x\u0302²'],
    ['\\hat{x^{\\alpha}}', 'x\u0302^α'],
    ['\\bar{x_1}', 'x\u0304₁'],
  ]));

describe('fractions and binomials', () =>
  cases([
    ['\\frac{a}{b}', 'a/b'],
    ['\\frac12', '1/2'],
    ['\\frac {a} {b}', 'a/b'],
    ['\\frac{\\pi}{4}', 'π/4'],
    ['\\dfrac{1}{x}', '1/x'],
    ['\\tfrac{3}{4}', '3/4'],
    ['\\frac{10}{3.5}', '10/3.5'],
    ['\\frac{x^2}{2}', 'x²/2'],
    ['\\frac{(a+b)}{2}', '(a+b)/2'],
    ['\\frac{(a+b)^2}{4}', '(a+b)²/4'],
    ['\\frac{(a)(b)}{2}', '((a)(b))/2'],
    ['\\frac{|x|}{2}', '|x|/2'],
    ['\\frac{-1}{2}', '(−1)/2'],
    ['\\frac{dy}{dx}', '(dy)/(dx)'],
    ['\\frac{\\text{거리}}{\\text{시간}}', '거리/시간'],
    ['\\frac{\\frac{a}{b}}{c}', '(a/b)/c'],
    ['\\frac{a}{b}^2', '(a/b)²'],
    ['\\frac{a}{b} + c', 'a/b + c'],
    ['\\frac{1}{2}x', '(1/2)x'],
    ['\\frac{1}{2} m v^2', '(1/2) m v²'],
    ['\\sin\\frac{\\pi}{2}', 'sin(π/2)'],
    ['1 / \\frac{a}{b}', '1 / (a/b)'],
    ['\\int_0^1 \\frac{1}{x}\\,dx', '∫₀¹ (1/x) dx'],
    ['\\binom{n+1}{2}', 'C(n+1, 2)'],
    // a number then a fraction is a mixed number; after another factor the fraction is bracketed
    ['2\\frac{1}{3}', '2 1/3'],
    ['-2\\frac{1}{2}', '−2 1/2'],
    ['1\\frac{1}{2}', '1 1/2'],
    ['x = 2 \\frac{1}{3}', 'x = 2 1/3'],
    ['2\\frac{x}{3}', '2x/3'],
    ['\\sqrt{3}\\frac{1}{2}', '√3(1/2)'],
    ['\\frac{1}{2}\\frac{1}{3}', '(1/2)(1/3)'],
    ['x\\frac{1}{2}', 'x(1/2)'],
    ['F = m\\frac{dv}{dt}', 'F = m(dv)/(dt)'],
    // ÷ groups from the left like /, and ! or % would take only the denominator
    ['\\frac{a}{b} \\div \\frac{c}{d}', 'a/b ÷ (c/d)'],
    ['a \\div \\frac{b}{c}', 'a ÷ (b/c)'],
    ['\\frac{3}{4} \\div 2', '3/4 ÷ 2'],
    ['\\frac{n}{2}!', '(n/2)!'],
    ['\\frac{1}{2}\\%', '(1/2)%'],
  ]));

describe('roots', () =>
  cases([
    ['\\sqrt{x}', '√x'],
    ['\\sqrt x', '√x'],
    ['\\sqrt{12}', '√12'],
    ['\\sqrt{x+1}', '√(x+1)'],
    ['\\sqrt{x^2}', '√(x²)'],
    ['\\sqrt{\\frac{a}{b}}', '√(a/b)'],
    ['\\sqrt[3]{x}', '∛x'],
    ['\\sqrt[4]{x}', '∜x'],
    ['\\sqrt[n]{x}', 'ⁿ√x'],
    ['\\sqrt[n]{x+1}', 'ⁿ√(x+1)'],
    ['\\sqrt[\\alpha]{x}', null],
    // a factor right after a root would read as under it
    ['\\sqrt{2}x', '√2·x'],
    ['\\sqrt{x}y', '√x·y'],
    ['\\sqrt{n}\\sigma', '√n·σ'],
    ['\\sqrt{2}a', '√2·a'],
    ['2\\sqrt{2}x', '2√2·x'],
    ['\\sqrt[3]{2}x', '∛2·x'],
    ['\\frac{\\sqrt{2}x}{2}', '(√2·x)/2'],
    ['\\sqrt{2} x', '√2 x'],
    ['\\sqrt{2}\\sqrt{3}', '√2√3'],
    ['\\sqrt{2}\\sin x', '√2 sin x'],
    ['\\frac{\\sqrt{2}}{2}', '√2/2'],
    // a script on a root goes outside brackets, as √x² would read as √(x²)
    ['\\sqrt{x}^2', '(√x)²'],
    ['\\sqrt{2}^3', '(√2)³'],
    ['\\sqrt{x+1}^2', '(√(x+1))²'],
    ['\\sqrt[3]{x}^2', '(∛x)²'],
  ]));

describe('superscripts and subscripts', () =>
  cases([
    ['\\sum_{i=1}^{n}', '∑ᵢ₌₁ⁿ'],
    ['10^{-3}', '10⁻³'],
    ['e^{i\\pi}', 'e^(iπ)'],
    ['x_{max}', 'xₘₐₓ'],
    ['\\mathbb{R}^n', 'ℝⁿ'],
    ['x^{abcdefghijklmnoprstuvwxyz}', 'xᵃᵇᶜᵈᵉᶠᵍʰⁱʲᵏˡᵐⁿᵒᵖʳˢᵗᵘᵛʷˣʸᶻ'],
    ['x^{ABDEGHIJKLMNOPRTUVW}', 'xᴬᴮᴰᴱᴳᴴᴵᴶᴷᴸᴹᴺᴼᴾᴿᵀᵁⱽᵂ'],
    ['x^{0123456789+-=()}', 'x⁰¹²³⁴⁵⁶⁷⁸⁹⁺⁻⁼⁽⁾'],
    ['x_{0123456789+-=()}', 'x₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎'],
    ['x_{aehijklmnoprstuvx}', 'xₐₑₕᵢⱼₖₗₘₙₒₚᵣₛₜᵤᵥₓ'],
    ['x^q', 'x^q'],
    ['x_b', 'x_b'],
    ['x_{b1}', 'x_(b1)'],
    ['x^{\\alpha}', 'x^α'],
    ['x_{\\text{max}}', 'xₘₐₓ'],
    ['x_{\\text{new}}', 'x_(new)'],
    ['f^{(n)}(x)', 'f⁽ⁿ⁾(x)'],
    ['x^10', 'x¹0'],
    ['{}^{14}C', '¹⁴C'],
    ['90^\\circ', '90°'],
    ['^{\\circ}C', '°C'],
    ['f^{\\prime}(x)', 'f′(x)'],
    ['f^\\prime', 'f′'],
    // no raised star exists: plain text writes x* (seen in answers on the test phone, 10-30)
    ['f(x^*)', 'f(x*)'],
    ['x^{*}', 'x*'],
    ['f^\\ast(x)', 'f*(x)'],
    ['A^\\dagger', 'A†'],
    ['x^{2*}', 'x^(2∗)'],
    ['e^{x^2}', 'e^(x²)'],
    ['e^{-x^2/2}', 'e^(−x²/2)'],
    ['a_{i_1}', 'a_(i₁)'],
    ['e^{\\frac{1}{2}}', 'e^(1/2)'],
    ['{a+b}^2', '(a+b)²'],
    ['x^{y^{\\pi z}}', null],
    ['x^2^3', null],
    ['x_1_2', null],
    ['x^', null],
    ['x^}', null],
  ]));

describe('what cannot be shown gives null', () =>
  cases([
    ['\\begin{cases} x \\end{cases}', null],
    ['\\end{array}', null],
    ['a \\\\ b', null],
    ['a & b', null],
    ['{a \\over b}', null],
    ['n \\choose k', null],
    ['a \\atop b', null],
    ['\\unknowncommand x', null],
    ['\\constructor', null],
    ['\\toString', null],
    ['{a', null],
    ['a}', null],
    ['}{', null],
    ['\\', null],
    ['a $ b', null],
    ['#1', null],
    ['', null],
    ['   ', null],
    ['\\,', null],
    ['\\frac{}{}', null],
    ['\\frac{}{2}', null],
    ['\\frac{1}{ }', null],
    ['\\binom{}{}', null],
    ['\\binom{n}{}', null],
    ['\\sqrt{}', null],
  ]));

describe('bounds', () => {
  test('input over 1000 characters gives null', () => {
    expect(texToUnicode('x'.repeat(1000))).toBe('x'.repeat(1000));
    expect(texToUnicode('x'.repeat(1001))).toBeNull();
  });

  test('nesting deeper than 20 gives null', () => {
    const nested = (n: number) => '{'.repeat(n) + 'x' + '}'.repeat(n);
    expect(texToUnicode(nested(20))).toBe('x');
    expect(texToUnicode(nested(21))).toBeNull();
    expect(texToUnicode('x^{'.repeat(30) + '2' + '}'.repeat(30))).toBeNull();
    expect(texToUnicode('\\sqrt{'.repeat(40) + 'x' + '}'.repeat(40))).toBeNull();
    expect(texToUnicode('\\left('.repeat(40) + 'x' + '\\right)'.repeat(40))).toBeNull();
    expect(texToUnicode('\\frac'.repeat(150) + '12')).toBeNull();
  });

  test('hostile input never throws and stays fast', () => {
    const hostile = [
      '{'.repeat(1000),
      '}'.repeat(1000),
      '\\'.repeat(1000),
      '^'.repeat(1000),
      "'".repeat(1000),
      ' '.repeat(1000),
      '\\left'.repeat(200),
      '\\sqrt['.repeat(160),
      'x_'.repeat(500),
      '\\frac{'.repeat(140),
      '(a+b)'.repeat(200),
      '\\, '.repeat(333),
      '1.'.repeat(500),
      '\u{1d465}^\u{1d466}'.repeat(150),
      '\ud800'.repeat(500),
      '\\text{'.repeat(140),
    ];
    const start = Date.now();
    for (const s of hostile) expect(() => texToUnicode(s)).not.toThrow();
    expect(Date.now() - start).toBeLessThan(1000);
    expect(texToUnicode(undefined as unknown as string)).toBeNull();
  });
});
