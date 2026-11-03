import { cp, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { minify } from 'terser';

const projectRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const defaultSourceDir = path.join(projectRoot, 'static');
const defaultOutputDir = path.join(projectRoot, 'dist', 'static');

function contains(parent, child) {
  const relative = path.relative(parent, child);
  return relative !== '' && !relative.startsWith(`..${path.sep}`) && relative !== '..' && !path.isAbsolute(relative);
}

function validateDirectories(sourceDir, outputDir) {
  const filesystemRoot = path.parse(outputDir).root;

  const directoriesOverlap = outputDir === sourceDir
    || contains(sourceDir, outputDir)
    || contains(outputDir, sourceDir);

  if (outputDir === filesystemRoot || directoriesOverlap) {
    throw new Error('Static source and output directories must be separate and must not contain one another.');
  }
}

async function findJavaScriptFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    const entryPath = path.join(directory, entry.name);

    if (entry.isDirectory()) {
      files.push(...await findJavaScriptFiles(entryPath));
    } else if (entry.isFile() && entry.name.endsWith('.js')) {
      files.push(entryPath);
    }
  }

  return files.sort();
}

export async function buildStatic({ sourceDir = defaultSourceDir, outputDir = defaultOutputDir } = {}) {
  const resolvedSource = path.resolve(sourceDir);
  const resolvedOutput = path.resolve(outputDir);

  validateDirectories(resolvedSource, resolvedOutput);

  const sourceStats = await stat(resolvedSource);
  if (!sourceStats.isDirectory()) {
    throw new Error(`Static source is not a directory: ${resolvedSource}`);
  }

  await rm(resolvedOutput, { recursive: true, force: true });
  await cp(resolvedSource, resolvedOutput, { recursive: true });

  const javaScriptFiles = await findJavaScriptFiles(resolvedOutput);
  const results = [];

  for (const file of javaScriptFiles) {
    const relativePath = path.relative(resolvedOutput, file).split(path.sep).join('/');

    if (file.endsWith('.min.js')) {
      results.push({ path: relativePath, status: 'preserved' });
      continue;
    }

    const source = await readFile(file, 'utf8');
    const output = await minify(source, {
      compress: { passes: 2 },
      ecma: 2020,
      format: { comments: false, ecma: 2020 },
      mangle: true,
    });

    if (!output.code) {
      throw new Error(`Terser produced no output for ${relativePath}`);
    }

    const minified = `${output.code}\n`;
    if (Buffer.byteLength(minified) >= Buffer.byteLength(source)) {
      throw new Error(`Minification did not reduce ${relativePath}`);
    }

    await writeFile(file, minified, 'utf8');
    results.push({
      path: relativePath,
      status: 'minified',
      before: Buffer.byteLength(source),
      after: Buffer.byteLength(minified),
    });
  }

  return results;
}

async function main() {
  const results = await buildStatic();

  for (const result of results) {
    if (result.status === 'minified') {
      console.log(`${result.path}: ${result.before} -> ${result.after} bytes`);
    } else {
      console.log(`${result.path}: preserved`);
    }
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
}
