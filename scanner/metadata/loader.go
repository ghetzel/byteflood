package metadata

type Loader interface {
    Type() string
    CanHandle(string) bool
    LoadMetadata(string) (map[string]interface{}, error)
}

func GetLoaders() []Loader {
    return []Loader{
        FileLoader{},
        AudioLoader{},
        VideoLoader{},
        ImageLoader{},
    }
}

func GetLoadersForFile(name string) []Loader {
    loaders := make([]Loader, 0)

    for _, loader := range GetLoaders() {
        if loader.CanHandle(name) {
            loaders = append(loaders, loader)
        }
    }

    return loaders
}
