package config

// Struct to hold the configuration
type Config struct {
	RTMP    RTMP         `mapstructure:"rtmp"`
	Service Service      `mapstructure:"service"`
	Docker  DockerConfig `mapstructure:"docker"`
	MP4     MP4          `mapstructure:"mp4"`
	EBML    EBML         `mapstructure:"ebml"`
	S3      S3           `mapstructure:"s3"`
}

type RTMP struct {
	Port int `mapstructure:"port"`
}

type Service struct {
	Port    int  `mapstructure:"port"`
	LLHLS   bool `mapstructure:"llhls"`
	DiskRam bool `mapstructure:"disk_ram"`
}

type DockerConfig struct {
	Mode bool `mapstructure:"mode"`
}

type MP4 struct {
	Record bool `mapstructure:"record"`
}

type EBML struct {
	Record bool `mapstructure:"record"`
}

type S3 struct {
	Access       string `mapstructure:"access"`
	Secret       string `mapstructure:"secret"`
	Region       string `mapstructure:"region"`
	CacheControl string `mapstructure:"cache_control"`
}
