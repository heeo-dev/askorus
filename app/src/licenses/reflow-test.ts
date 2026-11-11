// reflow() joins the lines of a license text that were wrapped for a wide
// screen, so a phone wraps them once. It must not lose or reorder a word.

import type { Licenses } from '@/app/licenses';
import { reflow } from '@/licenses/reflow';

declare const __dirname: string;
const { readFileSync }: { readFileSync(p: string, enc: 'utf8'): string } = require('node:fs');
const { join }: { join(...parts: string[]): string } = require('node:path');

const data = JSON.parse(readFileSync(join(__dirname, 'licenses.json'), 'utf8')) as Licenses;
const textOf = (name: string) => {
  const p = data.packages.find((x) => x.n === name);
  return p && p.t !== null ? data.texts[p.t] : '';
};
const words = (s: string) => s.split(/\s+/).filter(Boolean);

test('the wrapped lines of a paragraph become one line, paragraphs stay apart', () => {
  const mit = [
    'MIT License',
    '',
    'Copyright (c) 2014-present Sebastian McKenzie and other contributors',
    '',
    'Permission is hereby granted, free of charge, to any person obtaining',
    'a copy of this software and associated documentation files (the',
    '"Software"), to deal in the Software without restriction, including',
    'the following conditions:',
    '',
    'The above copyright notice and this permission notice shall be',
    'included in all copies or substantial portions of the Software.',
  ].join('\n');
  expect(reflow(mit).split('\n')).toEqual([
    'MIT License',
    '',
    'Copyright (c) 2014-present Sebastian McKenzie and other contributors',
    '',
    'Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including the following conditions:',
    '',
    'The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.',
  ]);
});

test('titles, copyright lines and list items keep their own lines', () => {
  const text = [
    '                                 Apache License',
    '                           Version 2.0, January 2004',
    '',
    'Copyright (c) 2015-present, A Long Name Of A Company, Inc. and others',
    'Copyright (c) 2016 Another Holder With A Fairly Long Name As Well',
    'All rights reserved.',
    '',
    'Redistribution and use in source and binary forms, with or without',
    'modification, are permitted provided that the following conditions are met:',
    '',
    '1. Redistributions of source code must retain the above copyright notice, this',
    '   list of conditions and the following disclaimer.',
    '2. Redistributions in binary form must reproduce the above copyright notice,',
    '   this list of conditions and the following disclaimer.',
    '',
    '      (a) You must give any other recipients of the Work or',
    '          Derivative Works a copy of this License; and',
    '      (b) You must cause any modified files to carry prominent notices',
    '',
    'Windows line ends are read too, and spaces at a line end are dropped.   \r',
    'This line follows a long one.',
  ].join('\n');
  expect(reflow(text).split('\n')).toEqual([
    '                                 Apache License',
    '                           Version 2.0, January 2004',
    '',
    'Copyright (c) 2015-present, A Long Name Of A Company, Inc. and others',
    'Copyright (c) 2016 Another Holder With A Fairly Long Name As Well',
    'All rights reserved.',
    '',
    'Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:',
    '',
    '1. Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer.',
    '2. Redistributions in binary form must reproduce the above copyright notice, this list of conditions and the following disclaimer.',
    '',
    '      (a) You must give any other recipients of the Work or Derivative Works a copy of this License; and',
    '      (b) You must cause any modified files to carry prominent notices',
    '',
    'Windows line ends are read too, and spaces at a line end are dropped. This line follows a long one.',
  ]);
});

test('no license text of the list loses, gains or moves a word', () => {
  for (const text of data.texts) {
    const shown = reflow(text);
    expect(words(shown)).toEqual(words(text));
    // Paragraphs are still apart: no blank line is added or dropped.
    const blanks = (s: string) => s.replace(/\r\n?/g, '\n').split('\n').filter((l) => l.trim() === '').length;
    expect(blanks(shown)).toBe(blanks(text));
  }
});

test('the texts the app shows most are joined as meant', () => {
  const mit = reflow(textOf('react'));
  expect(mit).toContain('Permission is hereby granted, free of charge, to any person obtaining a copy of this software');
  const apache = reflow(textOf('com.squareup.okhttp3:okhttp'));
  expect(apache).toMatch(/^\s*Apache License\n\s+Version 2\.0, January 2004\n/);
  expect(apache).toContain('as indicated by a copyright notice that is included in or attached to the work');
  expect(apache).toContain('royalty-free, irrevocable copyright license to reproduce');
  expect(apache).toContain('"License" shall mean the terms and conditions for use, reproduction, and distribution as defined by Sections 1 through 9 of this document.');
  expect(apache).toMatch(/\n\s+1\. Definitions\.\n/);
  const ofl = reflow(textOf('KaTeX fonts'));
  expect(ofl).toMatch(/\n-{20,}\nSIL OPEN FONT LICENSE Version 1\.1 - 26 February 2007\n-{20,}\n/);
  expect(ofl).toContain('and is also available with a FAQ at:\nhttps://openfontlicense.org\n');
  expect(ofl).toContain('to stimulate worldwide development of collaborative font projects');
});
