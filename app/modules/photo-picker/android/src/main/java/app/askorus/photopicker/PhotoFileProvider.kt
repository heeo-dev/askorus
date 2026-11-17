package app.askorus.photopicker

import androidx.core.content.FileProvider

// A provider of its own, so its authority and its one folder
// (res/xml/askorus_photo_paths.xml) never mix with another library's.
class PhotoFileProvider : FileProvider()
