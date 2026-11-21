import assert from 'node:assert/strict';
import test from 'node:test';

import { detect } from './check-licenses.mjs';

const bsd = (clauses) => `Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:
1. Redistributions of source code must retain the above copyright notice.
2. Redistributions in binary form must reproduce the above copyright notice.
${clauses}
THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS".`;

test('BSD-2-Clause is detected', () => {
  assert.equal(detect(bsd('')), 'BSD-2-Clause');
});

test('BSD-3-Clause is detected', () => {
  assert.equal(detect(bsd('3. Neither the name of the copyright holder nor the names of its contributors may be used to endorse or promote products derived from this software.')), 'BSD-3-Clause');
});

test('BSD-4-Clause is not misclassified as BSD-2-Clause', () => {
  const text = bsd(`3. All advertising materials mentioning features or use of this software must display the following acknowledgement.
4. Neither the name of the copyright holder nor the names of its contributors may be used to endorse or promote products derived from this software.`);
  assert.equal(detect(text), 'BSD-4-Clause');
});
