// SPDX-License-Identifier: Apache-2.0

/** separatorOf is the path separator a machine's paths use: \ on Windows. */
export function separatorOf(path: string): string {
  return /^[A-Za-z]:\\/.test(path) || path.startsWith('\\\\') ? '\\' : '/';
}

/** joinPath puts a name inside a folder, with the folder's own separator. */
export function joinPath(folder: string, name: string): string {
  const separator = separatorOf(folder);
  return folder.endsWith(separator) ? folder + name : folder + separator + name;
}

/** Crumb is one folder of a path, to jump back to. */
export interface Crumb {
  name: string;
  path: string;
}

/**
 * crumbs splits a path into the folders along it: "/srv/media" is "/",
 * "srv", "media"; "C:\Users" is "C:\", "Users".
 */
export function crumbs(path: string): Crumb[] {
  if (path === '') {
    return [];
  }
  const separator = separatorOf(path);
  const parts = path.split(separator).filter((part) => part !== '');
  const root = separator === '/' ? '/' : `${parts.shift() ?? ''}\\`;
  const result: Crumb[] = [{ name: root, path: root }];
  let current = root;
  for (const part of parts) {
    current = joinPath(current, part);
    result.push({ name: part, path: current });
  }
  return result;
}

/** isEditable says whether a file is small enough to open as text. */
export function isEditable(sizeBytes: number): boolean {
  return sizeBytes <= 1024 * 1024;
}
