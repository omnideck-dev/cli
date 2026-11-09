import assert from 'node:assert/strict';
import test from 'node:test';

import {
  isConventionalTitle,
  parseChangedFiles,
  parseFragment,
  renderReleaseNotes,
  validatePullRequest,
} from '../scripts/release-notes.mjs';

const validFragment = `---
type: added
area: desktop
---

Zoom the whole application while keeping tabs, menus, and previews aligned.
`;

test('accepts the organization Conventional Commit title format', () => {
  assert.equal(isConventionalTitle('feat(desktop): add native zoom'), true);
  assert.equal(isConventionalTitle('fix!: remove an unsafe fallback'), true);
  assert.equal(isConventionalTitle('Add native zoom'), false);
  assert.equal(isConventionalTitle('Feat(desktop): add native zoom'), false);
});

test('parses a valid Keep a Changelog fragment', () => {
  assert.deepEqual(parseFragment(validFragment, 'zoom.md'), {
    type: 'added',
    area: 'desktop',
    body: 'Zoom the whole application while keeping tabs, menus, and previews aligned.',
    source: 'zoom.md',
  });
});

test('rejects invalid fragment metadata', () => {
  assert.throws(
    () =>
      parseFragment(
        `---
type: feature
area: Desktop UI
---

Add zoom.
`,
        'bad.md',
      ),
    /type must be one of/,
  );
});

test('parses added, renamed, and deleted paths from git diff output', () => {
  assert.deepEqual(
    parseChangedFiles(
      'A\trelease-notes.d/zoom.md\nR100\told.md\trelease-notes.d/new.md\nD\tgone.md\n',
    ),
    [
      { status: 'A', path: 'release-notes.d/zoom.md' },
      { status: 'R100', path: 'release-notes.d/new.md' },
      { status: 'D', path: 'gone.md' },
    ],
  );
});

test('requires a fragment or an explicit no-note decision', () => {
  const errors = validatePullRequest({
    title: 'fix(cli): preserve configuration',
    changedFiles: [{ status: 'M', path: 'main.go' }],
  });
  assert.match(errors.join('\n'), /Add a release-notes/);
});

test('accepts and validates a new fragment', () => {
  const errors = validatePullRequest(
    {
      title: 'feat(desktop): add native zoom',
      changedFiles: [{ status: 'A', path: 'release-notes.d/native-zoom.md' }],
    },
    { readFragment: () => validFragment },
  );
  assert.deepEqual(errors, []);
});

test('requires a reason for release-note:none', () => {
  const errors = validatePullRequest({
    title: 'ci(desktop): expand package checks',
    body: '## Release note\n\nNone: Build qualification only; no user-visible behavior changed.\n',
    labels: ['release-note:none'],
    changedFiles: [{ status: 'M', path: '.github/workflows/desktop.yml' }],
  });
  assert.deepEqual(errors, []);
});

test('Dependabot dependency-only updates need no release-note decision', () => {
  const update = {
    title: 'chore(deps): bump the go-dependencies group with 2 updates',
    author: 'dependabot[bot]',
    changedFiles: [
      { status: 'M', path: 'go.mod' },
      { status: 'M', path: 'go.sum' },
      { status: 'M', path: '.github/workflows/release.yml' },
    ],
  };
  assert.deepEqual(validatePullRequest(update), []);

  // The title rule still applies.
  assert.match(
    validatePullRequest({ ...update, title: 'Bump x from 1 to 2' }).join('\n'),
    /Conventional Commits/,
  );
  // Any other path needs the usual decision.
  assert.match(
    validatePullRequest({
      ...update,
      changedFiles: [...update.changedFiles, { status: 'M', path: 'cmd/root.go' }],
    }).join('\n'),
    /Add a release-notes/,
  );
  // Only the Dependabot app account is exempt.
  assert.match(
    validatePullRequest({ ...update, author: 'someone' }).join('\n'),
    /Add a release-notes/,
  );
  assert.match(
    validatePullRequest({
      ...update,
      changedFiles: [{ status: 'M', path: '.github/workflows/nested/x.yml' }],
    }).join('\n'),
    /Add a release-notes/,
  );
});

test('features and breaking changes cannot opt out of release notes', () => {
  const featureErrors = validatePullRequest({
    title: 'feat(chat): add folders',
    body: '## Release note\n\nNone: Internal only.\n',
    labels: ['release-note:none'],
  });
  assert.match(featureErrors.join('\n'), /Features and breaking changes/);

  const breakingErrors = validatePullRequest({
    title: 'fix(api)!: remove legacy fields',
    body: '## Release note\n\nNone: Internal only.\n',
    labels: ['release-note:none'],
  });
  assert.match(breakingErrors.join('\n'), /Features and breaking changes/);
});

test('renders grouped human-facing release notes', () => {
  const output = renderReleaseNotes(
    [
      parseFragment(validFragment, 'zoom.md'),
      parseFragment(
        `---
type: fixed
area: setup
---

Recover automatically when the saved port is already in use.
`,
        'port.md',
      ),
    ],
    'v1.2.0',
  );

  assert.match(output, /^# Release notes for v1\.2\.0/m);
  assert.match(output, /^## Added$/m);
  assert.match(output, /^- \*\*Desktop:\*\* Zoom the whole application/m);
  assert.match(output, /^## Fixed$/m);
});
