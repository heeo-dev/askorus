// The app's native code is compiled with the path of each C/C++ source in
// it (__FILE__ in asserts), and react-native-screens' code generated for the
// new architecture put the whole path of the PC's checkout into
// libreact_codegen_rnscreens.so (#63). -ffile-prefix-map writes the app
// folder as "app" instead, in every C and C++ file the app's CMake builds
// (the autolinked libraries' code too). No other compiler setting changes.

const marker = '// askorus: file-prefix-map (#63)';

// Groovy inside the app's build.gradle: the app folder with forward
// slashes, as the compiler is handed the paths on every system.
const block = [
  `        ${marker}`,
  '        externalNativeBuild {',
  '            cmake {',
  "                def askorusAppDir = rootDir.parentFile.absolutePath.replace('\\\\', '/')",
  '                cFlags "-ffile-prefix-map=${askorusAppDir}=app"',
  '                cppFlags "-ffile-prefix-map=${askorusAppDir}=app"',
  '            }',
  '        }',
].join('\n');

/**
 * addPathMap puts the path map into the defaultConfig of the app's
 * build.gradle, once. A file without a defaultConfig is given back as it is.
 */
function addPathMap(gradle) {
  if (gradle.includes(marker)) return gradle;
  const at = gradle.search(/defaultConfig\s*\{[^\n]*\n/);
  if (at < 0) return gradle;
  const end = gradle.indexOf('\n', at) + 1;
  return gradle.slice(0, end) + block + '\n' + gradle.slice(end);
}

module.exports = { addPathMap, marker };
