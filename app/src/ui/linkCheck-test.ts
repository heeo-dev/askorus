// Where a link in an answer really goes (#47): the host it leads to, and
// whether the words shown name another site.

import { linkHost, shownElsewhere } from './linkCheck';

test('linkHost is where an http(s) address leads', () => {
  expect(linkHost('https://Example.com/a?b#c')).toBe('example.com');
  expect(linkHost('http://www.example.com:8080/')).toBe('example.com');
  expect(linkHost('https://example.com.')).toBe('example.com');
  // What stands before "@" is a user name, not the site.
  expect(linkHost('https://mybank.com@evil.example/login')).toBe('evil.example');
  expect(linkHost('https://mybank.com:pw@evil.example')).toBe('evil.example');
  // An "@" after the first "/" is part of the path.
  expect(linkHost('https://example.com/@user')).toBe('example.com');
  expect(linkHost('mailto:a@example.com')).toBeNull();
  expect(linkHost('javascript:alert(1)')).toBeNull();
  expect(linkHost('https://')).toBeNull();
});

test('words that name another site than the link are told', () => {
  expect(shownElsewhere('mybank.com', 'https://evil.example/')).toBe('mybank.com');
  expect(shownElsewhere('https://www.mybank.com/login', 'https://evil.example/login')).toBe('mybank.com');
  expect(shownElsewhere('Sign in at mybank.co.kr today', 'http://evil.example')).toBe('mybank.co.kr');
  expect(shownElsewhere('mybank.com', 'https://mybank.com@evil.example/')).toBe('mybank.com');
  // A look-alike ending is another site.
  expect(shownElsewhere('example.com', 'https://example.com.evil.example/')).toBe('example.com');
  expect(shownElsewhere('example.com', 'https://notexample.com/')).toBe('example.com');
});

test('words that name no site, or the same one, are not', () => {
  expect(shownElsewhere('출처', 'https://example.com/a')).toBeNull();
  expect(shownElsewhere('the docs (v2.1)', 'https://example.com/a')).toBeNull();
  expect(shownElsewhere('example.com', 'https://example.com/a')).toBeNull();
  expect(shownElsewhere('https://www.example.com/a', 'https://example.com/a')).toBeNull();
  expect(shownElsewhere('Example.COM', 'https://www.example.com/')).toBeNull();
  // A site and its subdomains are one place, either way round.
  expect(shownElsewhere('example.com', 'https://docs.example.com/')).toBeNull();
  expect(shownElsewhere('docs.example.com', 'https://example.com/docs')).toBeNull();
  // Several sites named, one of them the link's own.
  expect(shownElsewhere('example.com (mirror of other.org)', 'https://example.com/')).toBeNull();
  expect(shownElsewhere('anything', 'javascript:alert(1)')).toBeNull();
});
