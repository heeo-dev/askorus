"""Checks the judge-eval data files against the P0-6 set (P2-3 S4).

Run from anywhere: python -I docs/judge/check-eval-data.py

It reads the files next to it: questions.json, answers-2026-10-30.json,
pairs-2026-10-30.json, eval-split.json, eval-groups.json and
eval-evidence.json. It uses the standard library only and does not share
code with the Go loader in agent/cmd/judge-eval, so the two check each
other. It prints OK, or every problem it finds and exits with 1.

- Split: dev and test are disjoint and cover questions 1..30, and every
  count recomputes from the questions and pairs files to the stored one
  and to the numbers the S4 plan fixed.
- Groups: hard is the pairs with hard true, boundary is the rule
  recomputed (doubt, or review with a label other than same; hard
  included), both in pairs-file order, with the counts 18/123/491.
- Evidence: 51 entries, the 21 opposite pairs in pairs-file order and then
  one same pair per question by the SHA-256 rule; labels equal the pairs
  file; 1 to 3 distinct non-empty strings each, without the Markdown the
  text rule leaves out; every string, normalized, is in the normalized
  against answer. Normalizing drops a link [t](u) to t, then removes white
  space and the characters |*_`~ (splitNorm in
  agent/internal/judge/split_test.go).
"""

import hashlib
import json
import os
import re
import sys
import unicodedata

sys.stdout.reconfigure(encoding='utf-8')

HERE = os.path.dirname(os.path.abspath(__file__))
SET = 'P0-6'
QUESTIONS = 'questions.json'
ANSWERS = 'answers-2026-10-30.json'
PAIRS = 'pairs-2026-10-30.json'
AIS = ('claude', 'codex', 'opencode')

# The numbers the S4 plan fixed (docs/phase2-plan.md, S4).
WANT_SPLIT = {
    'dev': {'questions': 15, 'english': 4, 'claims': 152, 'pairs': 304, 'opposite': 11, 'hard': 8},
    'test': {'questions': 15, 'english': 4, 'claims': 155, 'pairs': 310, 'opposite': 10, 'hard': 10},
}
WANT_DEV = [1, 2, 4, 7, 10, 12, 13, 15, 17, 19, 21, 23, 25, 28, 30]
WANT_GROUPS = {'hard': 18, 'boundary': 123, 'clear': 491}
WANT_OPPOSITE = 21
WANT_SAME = 30

problems = []


def problem(msg):
    problems.append(msg)


def no_duplicate_keys(items):
    obj = {}
    for k, v in items:
        if k in obj:
            raise ValueError('duplicate key %r' % k)
        obj[k] = v
    return obj


def load(name):
    path = os.path.join(HERE, name)
    try:
        with open(path, 'rb') as f:
            raw = f.read()
    except OSError as e:
        problem('%s: cannot read (%s)' % (name, e.__class__.__name__))
        return None
    if raw.startswith(b'\xef\xbb\xbf'):
        problem('%s: starts with a BOM' % name)
        raw = raw[3:]
    if name.startswith('eval-'):
        if b'\r' in raw:
            problem('%s: has CR line endings, want LF' % name)
        if not raw.endswith(b'\n'):
            problem('%s: does not end with a line break' % name)
    try:
        return json.loads(raw.decode('utf-8'), object_pairs_hook=no_duplicate_keys)
    except (UnicodeDecodeError, ValueError) as e:
        problem('%s: not valid JSON (%s)' % (name, e))
        return None


def header(name, doc, want):
    for key, value in want.items():
        if doc.get(key) != value:
            problem('%s: %s is %r, want %r' % (name, key, doc.get(key), value))
    for key in ('made', 'about'):
        if not isinstance(doc.get(key), str) or not doc.get(key).strip():
            problem('%s: %s is missing or empty' % (name, key))


def int_list(name, key, value):
    if not isinstance(value, list) or not all(isinstance(x, int) and not isinstance(x, bool) for x in value):
        problem('%s: %s is not a list of numbers' % (name, key))
        return []
    if len(set(value)) != len(value):
        problem('%s: %s has repeated questions' % (name, key))
    return value


def str_list(name, key, value):
    if not isinstance(value, list) or not all(isinstance(x, str) for x in value):
        problem('%s: %s is not a list of ids' % (name, key))
        return []
    if len(set(value)) != len(value):
        problem('%s: %s has repeated ids' % (name, key))
    return value


# splitNorm (agent/internal/judge/split_test.go) after links are dropped to
# their text: Go's unicode.IsSpace is the ASCII spaces, U+0085, U+00A0 and
# the Z categories.
LINK = re.compile(r'!?\[([^\]]*)\]\([^)]*\)')
GO_SPACE = set('\t\n\v\f\r \x85\xa0')


def norm(s):
    s = LINK.sub(r'\1', s)
    return ''.join(ch for ch in s if ch not in GO_SPACE and unicodedata.category(ch) not in ('Zs', 'Zl', 'Zp') and ch not in '|*_`~')


def boundary(p):
    return p['doubt'] is True or (p['review'] is True and p['label'] != 'same')


def main():
    questions = load(QUESTIONS)
    answers = load(ANSWERS)
    pairs_doc = load(PAIRS)
    split = load('eval-split.json')
    groups = load('eval-groups.json')
    evidence = load('eval-evidence.json')
    if questions is None or answers is None or pairs_doc is None:
        return

    lang = {q['id']: q['lang'] for q in questions['questions']}
    if sorted(lang) != list(range(1, 31)):
        problem('%s: questions are not 1..30' % QUESTIONS)
    claims = pairs_doc['claims']
    pairs = pairs_doc['pairs']
    by_id = {p['id']: p for p in pairs}
    if len(by_id) != len(pairs):
        problem('%s: repeated pair ids' % PAIRS)
    answer = {}
    for a in answers['answers']:
        answer[(a['id'], a['ai'])] = a['answer']

    # Split.
    if split is not None:
        header('eval-split.json', split, {'set': SET, 'pairs': PAIRS})
        dev = int_list('eval-split.json', 'dev', split.get('dev'))
        test = int_list('eval-split.json', 'test', split.get('test'))
        if set(dev) & set(test):
            problem('eval-split.json: dev and test share questions %s' % sorted(set(dev) & set(test)))
        if sorted(dev + test) != list(range(1, 31)):
            problem('eval-split.json: dev and test do not cover questions 1..30 exactly once')
        if dev != sorted(dev) or test != sorted(test):
            problem('eval-split.json: dev or test is not in ascending order')
        if dev != WANT_DEV:
            problem('eval-split.json: dev is %s, want %s' % (dev, WANT_DEV))
        stored = split.get('counts') if isinstance(split.get('counts'), dict) else {}
        for part, qs in (('dev', dev), ('test', test)):
            got = {
                'questions': len(qs),
                'english': sum(1 for q in qs if lang.get(q) == 'en'),
                'claims': sum(1 for c in claims if c['q'] in qs),
                'pairs': sum(1 for p in pairs if p['q'] in qs),
                'opposite': sum(1 for p in pairs if p['q'] in qs and p['label'] == 'opposite'),
                'hard': sum(1 for p in pairs if p['q'] in qs and p['hard'] is True),
            }
            if got != WANT_SPLIT[part]:
                problem('eval-split.json: %s recomputes to %s, the plan fixed %s' % (part, got, WANT_SPLIT[part]))
            if stored.get(part) != got:
                problem('eval-split.json: counts.%s is %s, recomputed %s' % (part, stored.get(part), got))
        if set(stored) != {'dev', 'test'}:
            problem('eval-split.json: counts has keys %s, want dev and test' % sorted(stored))

    # Groups.
    hard_ids = [p['id'] for p in pairs if p['hard'] is True]
    boundary_ids = [p['id'] for p in pairs if boundary(p) or p['hard'] is True]
    if not set(hard_ids) <= {p['id'] for p in pairs if boundary(p)}:
        problem('%s: a hard pair is outside the boundary rule' % PAIRS)
    boundary_set = set(boundary_ids)
    clear = [p for p in pairs if p['id'] not in boundary_set]
    got_counts = {'hard': len(hard_ids), 'boundary': len(boundary_ids), 'clear': len(clear)}
    if got_counts != WANT_GROUPS:
        problem('eval-groups.json: the rule recomputes to %s, the plan fixed %s' % (got_counts, WANT_GROUPS))
    if groups is not None:
        header('eval-groups.json', groups, {'set': SET, 'pairs': PAIRS})
        if not isinstance(groups.get('rule'), str) or not groups.get('rule').strip():
            problem('eval-groups.json: rule is missing or empty')
        if groups.get('counts') != got_counts:
            problem('eval-groups.json: counts is %s, recomputed %s' % (groups.get('counts'), got_counts))
        hard = str_list('eval-groups.json', 'hard', groups.get('hard'))
        bnd = str_list('eval-groups.json', 'boundary', groups.get('boundary'))
        if hard != hard_ids:
            problem('eval-groups.json: hard is not the pairs with hard true in pairs-file order (extra %s, missing %s)'
                    % (sorted(set(hard) - set(hard_ids)), sorted(set(hard_ids) - set(hard))))
        if bnd != boundary_ids:
            problem('eval-groups.json: boundary is not the rule in pairs-file order (extra %s, missing %s)'
                    % (sorted(set(bnd) - boundary_set), sorted(boundary_set - set(bnd))))
        if not set(hard) <= set(bnd):
            problem('eval-groups.json: hard pairs outside boundary: %s' % sorted(set(hard) - set(bnd)))
        if 'clear' in groups:
            problem('eval-groups.json: clear is listed; it is every pair not in boundary')

    # Evidence.
    opposite_ids = [p['id'] for p in pairs if p['label'] == 'opposite']
    if len(opposite_ids) != WANT_OPPOSITE:
        problem('%s: %d opposite pairs, want %d' % (PAIRS, len(opposite_ids), WANT_OPPOSITE))
    same_ids = []
    for q in range(1, 31):
        cand = [p['id'] for p in clear if p['q'] == q and p['label'] == 'same']
        if not cand:
            problem('question %d: no clear same pair' % q)
            continue
        same_ids.append(min(cand, key=lambda i: hashlib.sha256(i.encode('utf-8')).hexdigest()))
    if evidence is None:
        return
    header('eval-evidence.json', evidence, {'set': SET, 'pairsFile': PAIRS, 'answers': ANSWERS})
    for key in ('text', 'same'):
        if not isinstance(evidence.get(key), str) or not evidence.get(key).strip():
            problem('eval-evidence.json: %s is missing or empty' % key)
    entries = evidence.get('pairs')
    if not isinstance(entries, list):
        problem('eval-evidence.json: pairs is not a list')
        return
    ids = [e.get('id') if isinstance(e, dict) else None for e in entries]
    want_ids = opposite_ids + same_ids
    if len(entries) != WANT_OPPOSITE + WANT_SAME:
        problem('eval-evidence.json: %d entries, want %d' % (len(entries), WANT_OPPOSITE + WANT_SAME))
    if len(set(ids)) != len(ids):
        problem('eval-evidence.json: repeated ids')
    if ids[:len(opposite_ids)] != opposite_ids:
        problem('eval-evidence.json: the first entries are not the %d opposite pairs in pairs-file order' % len(opposite_ids))
    if ids[len(opposite_ids):] != same_ids:
        problem('eval-evidence.json: the entries after the opposite pairs are not the same pairs by the SHA-256 rule, by question'
                ' (extra %s, missing %s)' % (sorted(set(ids[len(opposite_ids):]) - set(same_ids), key=str),
                                             sorted(set(same_ids) - set(ids[len(opposite_ids):]))))
    if ids != want_ids and set(ids) == set(want_ids):
        problem('eval-evidence.json: the ids are right but not in order')
    marker = re.compile(r'^([-*+]|[0-9]{1,3}[.)])[ \t]')
    for e in entries:
        if not isinstance(e, dict):
            problem('eval-evidence.json: an entry is not an object')
            continue
        pid = e.get('id')
        if set(e) != {'id', 'label', 'evidence'}:
            problem('eval-evidence.json: %s has keys %s, want id, label, evidence' % (pid, sorted(e)))
        p = by_id.get(pid)
        if p is None:
            problem('eval-evidence.json: %s is not a pair of %s' % (pid, PAIRS))
            continue
        if e.get('label') != p['label']:
            problem('eval-evidence.json: %s label %r, the pairs file says %r' % (pid, e.get('label'), p['label']))
        ev = e.get('evidence')
        if not isinstance(ev, list) or not 1 <= len(ev) <= 3:
            problem('eval-evidence.json: %s evidence is not a list of 1 to 3 strings' % pid)
            continue
        text = answer.get((p['q'], p['against']))
        if text is None:
            problem('eval-evidence.json: %s has no %s answer to question %d' % (pid, p['against'], p['q']))
            continue
        whole = norm(text)
        seen = set()
        for n, s in enumerate(ev, 1):
            where = '%s evidence %d' % (pid, n)
            if not isinstance(s, str) or not s.strip():
                problem('eval-evidence.json: %s is empty or not a string' % where)
                continue
            if s != s.strip():
                problem('eval-evidence.json: %s has white space at an end' % where)
            if '**' in s or '__' in s or '`' in s or LINK.search(s) or marker.match(s):
                problem('eval-evidence.json: %s keeps Markdown the text rule leaves out' % where)
            key = norm(s)
            if key in seen:
                problem('eval-evidence.json: %s repeats an earlier entry' % where)
            seen.add(key)
            if not key or key not in whole:
                problem('eval-evidence.json: %s is not in the %s answer to question %d' % (where, p['against'], p['q']))
    same_entries = [by_id[i] for i in ids[len(opposite_ids):] if i in by_id]
    dev = set(WANT_DEV)
    got = (sum(1 for p in same_entries if p['q'] in dev), sum(1 for p in same_entries if p['q'] not in dev),
           sum(1 for p in same_entries if lang.get(p['q']) == 'ko'), sum(1 for p in same_entries if lang.get(p['q']) == 'en'))
    if got != (15, 15, 22, 8):
        problem('eval-evidence.json: same pairs are %d dev / %d test, %d ko / %d en; want 15/15, 22/8' % got)


if __name__ == '__main__':
    try:
        main()
    except (KeyError, TypeError, AttributeError) as e:
        problem('unexpected file shape: %s %s' % (e.__class__.__name__, e))
    if problems:
        for msg in problems:
            print(msg)
        sys.exit(1)
    print('OK')
