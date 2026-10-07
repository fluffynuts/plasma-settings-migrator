package components

func themeCategory() *Category {
	return &Category{
		ID:    "theme",
		Label: "Theme",
		Subs: []*SubComponent{
			{ID: "color-scheme", Label: "Color scheme", Collect: collectColorScheme, Apply: applyGeneric},
			{ID: "icon-theme", Label: "Icon theme", Collect: collectIconTheme, Apply: applyGeneric},
			{ID: "fonts", Label: "Fonts", Collect: collectFonts, Apply: applyGeneric},
			{ID: "widget-style", Label: "Widget style", Collect: collectWidgetStyle, Apply: applyGeneric},
			{ID: "gtk-style", Label: "Gtk style", Collect: collectGtk, Apply: applyGeneric},
		},
	}
}

// The color scheme is spread over several groups of kdeglobals: the
// colour sets themselves, the effects for inactive and disabled widgets,
// the window decoration's title bar colours, and a few General and KDE
// keys. (Keys as written by plasma-workspace's colors KCM.)
func collectColorScheme(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "",
		KeySpec{File: "kdeglobals", Group: "Colors:*"},
		KeySpec{File: "kdeglobals", Group: "ColorEffects:*"},
		KeySpec{File: "kdeglobals", Group: "WM", Keys: []string{
			"activeBackground", "activeForeground", "inactiveBackground", "inactiveForeground",
			"activeBlend", "inactiveBlend",
		}},
		KeySpec{File: "kdeglobals", Group: "General", Keys: []string{
			"ColorScheme", "ColorSchemeHash", "AccentColor", "LastUsedCustomAccentColor", "accentColorFromWallpaper",
		}},
		KeySpec{File: "kdeglobals", Group: "KDE", Keys: []string{"contrast", "frameContrast"}},
	)
	if err != nil {
		return nil, err
	}
	f := &Fragment{Keys: keys}
	// A scheme the user installed is a file of its own, which the target
	// needs for the name above to resolve.
	if name := valueOf(keys, "General", "ColorScheme"); name != "" {
		f.Files = existingFiles(env, FileRef{Root: "data", Path: "color-schemes/" + name + ".colors"})
	}
	return f, nil
}

func collectIconTheme(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "", KeySpec{File: "kdeglobals", Group: "Icons", Keys: []string{"Theme"}})
	if err != nil {
		return nil, err
	}
	f := &Fragment{Keys: keys}
	if name := valueOf(keys, "Icons", "Theme"); name != "" {
		f.Files = existingFiles(env, FileRef{Root: "data", Path: "icons/" + name})
	}
	return f, nil
}

func collectFonts(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "",
		KeySpec{File: "kdeglobals", Group: "General", Keys: []string{
			"font", "fixed", "smallestReadableFont", "toolBarFont", "menuFont",
		}},
		KeySpec{File: "kdeglobals", Group: "WM", Keys: []string{"activeFont"}},
	)
	return &Fragment{Keys: keys}, err
}

func collectWidgetStyle(env *Env) (*Fragment, error) {
	keys, err := collectKeys(env, "",
		KeySpec{File: "kdeglobals", Group: "KDE", Keys: []string{
			"widgetStyle", "unionStyle", "ShowIconsOnPushButtons", "ShowIconsInMenuItems",
		}},
		KeySpec{File: "kdeglobals", Group: "Toolbar style", Keys: []string{
			"ToolButtonStyle", "ToolButtonStyleOtherToolbars",
		}},
	)
	return &Fragment{Keys: keys}, err
}

func collectGtk(env *Env) (*Fragment, error) {
	return &Fragment{Files: existingFiles(env,
		FileRef{Root: "config", Path: "gtk-3.0/settings.ini"},
		FileRef{Root: "config", Path: "gtk-4.0/settings.ini"},
		FileRef{Root: "home", Path: ".gtkrc-2.0"},
	)}, nil
}

func valueOf(keys []KeyValue, group, key string) string {
	for _, kv := range keys {
		if kv.Group == group && kv.Key == key {
			return kv.Value
		}
	}
	return ""
}
