// Expo config plugin: the app's native code is compiled without the path of
// the PC it was built on (#63, sourcePathMap.js). Runs at prebuild, so a new
// android folder gets it too.

const { withAppBuildGradle } = require('expo/config-plugins');

const { addPathMap } = require('./sourcePathMap');

module.exports = function withSourcePathMap(config) {
  return withAppBuildGradle(config, (c) => {
    if (c.modResults.language === 'groovy') c.modResults.contents = addPathMap(c.modResults.contents);
    return c;
  });
};
