package localmode

import "os"

const productModeEnv = "MULTICA_PRODUCT_MODE"

type Config struct {
	ProductMode string
}

func FromEnv() Config {
	return Config{
		ProductMode: os.Getenv(productModeEnv),
	}
}

func (c Config) Enabled() bool {
	return c.ProductMode == "local"
}
