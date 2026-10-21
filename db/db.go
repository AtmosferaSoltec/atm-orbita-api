// Package db embebe las migraciones en el binario: la imagen de producción no
// lleva el código fuente, y el servicio migrate las aplica desde aquí.
package db

import "embed"

// Migrations son los archivos de db/migrations.
//
//go:embed migrations
var Migrations embed.FS
