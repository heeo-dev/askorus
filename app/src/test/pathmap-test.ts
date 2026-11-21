// #63: the app's native code is compiled without the path of the PC it was
// built on. The config plugin puts -ffile-prefix-map into the defaultConfig
// of the app's build.gradle, once.

// eslint-disable-next-line @typescript-eslint/no-require-imports
const { addPathMap, marker } = require('../../plugins/sourcePathMap') as { addPathMap: (g: string) => string; marker: string };

const gradle = `android {
    namespace 'app.askorus'
    defaultConfig {
        applicationId 'app.askorus'
        minSdkVersion rootProject.ext.minSdkVersion
    }
}
`;

test('the path map goes into defaultConfig once, for C and C++', () => {
  const once = addPathMap(gradle);
  expect(once).toContain(marker);
  expect(once.indexOf(marker)).toBeGreaterThan(once.indexOf('defaultConfig {'));
  expect(once.indexOf(marker)).toBeLessThan(once.indexOf("applicationId 'app.askorus'"));
  expect(once).toContain('cFlags "-ffile-prefix-map=${askorusAppDir}=app"');
  expect(once).toContain('cppFlags "-ffile-prefix-map=${askorusAppDir}=app"');
  // The folder with forward slashes, as the compiler is given the paths.
  expect(once).toContain("rootDir.parentFile.absolutePath.replace('\\\\', '/')");
  expect(addPathMap(once)).toBe(once);
  expect(addPathMap('android {}')).toBe('android {}');
});

test('app.json runs the plugin', () => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const app = require('../../app.json') as { expo: { plugins: unknown[] } };
  expect(app.expo.plugins).toContain('./plugins/withSourcePathMap');
});
