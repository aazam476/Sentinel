// SPDX-License-Identifier: Apache-2.0
//
// Checks the license of every installed npm package under <project>/node_modules
// and exits with status 1 if any is GPL-family licensed or has no license.
//
// Usage: node scripts/check-npm-licenses.mjs <project dir>
//
// A package with a choice of licenses ("(MIT OR GPL-3.0)") passes as long as
// one of the choices is allowed, because we can take it under that one.

import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

const deniedPattern = /\b(A|L)?GPL\b|GNU/i;

/** Returns the license expression from a package.json, or '' if it has none. */
function licenseOf(packageJson) {
  if (typeof packageJson.license === 'string') {
    return packageJson.license;
  }
  // Old packages use an object or a "licenses" array instead.
  if (packageJson.license && typeof packageJson.license.type === 'string') {
    return packageJson.license.type;
  }
  if (Array.isArray(packageJson.licenses)) {
    return packageJson.licenses.map((entry) => entry.type).join(' OR ');
  }
  return '';
}

/** Reports whether at least one choice in a license expression is allowed. */
function isAllowed(license) {
  const choices = license.replace(/[()]/g, '').split(/\s+OR\s+/i);
  return choices.some((choice) => choice.trim() !== '' && !deniedPattern.test(choice));
}

/** Lists the package folders directly inside a node_modules folder. */
function packageFolders(nodeModulesDir) {
  const folders = [];
  for (const entry of readdirSync(nodeModulesDir, { withFileTypes: true })) {
    if (!entry.isDirectory() || entry.name.startsWith('.')) {
      continue;
    }
    const entryPath = join(nodeModulesDir, entry.name);
    if (entry.name.startsWith('@')) {
      for (const scoped of readdirSync(entryPath, { withFileTypes: true })) {
        if (scoped.isDirectory()) {
          folders.push(join(entryPath, scoped.name));
        }
      }
    } else {
      folders.push(entryPath);
    }
  }
  return folders;
}

/** Checks every package under nodeModulesDir, including nested node_modules. */
function checkAll(nodeModulesDir, problems, checkedCount) {
  for (const folder of packageFolders(nodeModulesDir)) {
    const manifestPath = join(folder, 'package.json');
    if (existsSync(manifestPath)) {
      const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
      const license = licenseOf(manifest);
      checkedCount.value += 1;
      if (license === '') {
        problems.push(`UNKNOWN  ${manifest.name}@${manifest.version} declares no license`);
      } else if (!isAllowed(license)) {
        problems.push(`DENIED   ${manifest.name}@${manifest.version} is ${license}`);
      }
    }
    const nested = join(folder, 'node_modules');
    if (existsSync(nested)) {
      checkAll(nested, problems, checkedCount);
    }
  }
}

const projectDir = process.argv[2] ?? '.';
const nodeModulesDir = join(projectDir, 'node_modules');
if (!existsSync(nodeModulesDir)) {
  console.error(`  ${nodeModulesDir} not found; run npm ci first`);
  process.exit(1);
}

const problems = [];
const checkedCount = { value: 0 };
checkAll(nodeModulesDir, problems, checkedCount);

for (const problem of problems) {
  console.log(`  ${problem}`);
}
console.log(`  checked ${checkedCount.value} packages, ${problems.length} problem(s)`);
process.exit(problems.length > 0 ? 1 : 0);
