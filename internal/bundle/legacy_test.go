package bundle

import "testing"

// v1LaravelCompose mirrors what bash Hull actually generated (yq-merged
// base + postgres + redis, map-form env and labels).
const v1LaravelCompose = `services:
  app:
    image: serversideup/php:8.2-fpm-nginx
    volumes:
      - ./:/var/www/html
    labels:
      caddy: shop.test
      caddy.reverse_proxy: "{{upstreams 8080}}"
      caddy.tls: internal
    networks:
      - default
      - caddy
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: postgres
      POSTGRES_DB: shop_db
      POSTGRES_HOST_AUTH_METHOD: trust
    volumes:
      - db_data:/var/lib/postgresql/data
  redis:
    image: redis:alpine
networks:
  caddy:
    external: true
volumes:
  db_data:
`

const v1WordpressCompose = `services:
  wordpress:
    image: wordpress:6.4
    environment:
      WORDPRESS_DB_HOST: db
      WORDPRESS_DB_NAME: blog_db
      WORDPRESS_DB_PASSWORD: ""
    labels:
      caddy: blog.test
  db:
    image: mariadb:lts
    environment:
      MYSQL_ALLOW_EMPTY_PASSWORD: "yes"
      MYSQL_DATABASE: hull_db
`

func TestDetectLegacyLaravel(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "compose.yaml", v1LaravelCompose)

	info, err := DetectLegacy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Template != "laravel" || info.PHP != "8.2" {
		t.Errorf("template/php = %s/%s", info.Template, info.PHP)
	}
	if info.DB != "postgres" || info.DBVersion != "" {
		t.Errorf("db = %s@%q (16 is the default, should collapse)", info.DB, info.DBVersion)
	}
	if info.Database != "shop_db" || !info.Redis || info.Host != "shop.test" {
		t.Errorf("info = %+v", info)
	}
}

func TestDetectLegacyWordpress(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "docker-compose.yml", v1WordpressCompose)

	info, err := DetectLegacy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Template != "wordpress" || info.DB != "mariadb" {
		t.Errorf("info = %+v", info)
	}
	if info.Database != "blog_db" {
		t.Errorf("database = %q (WORDPRESS_DB_NAME wins over db env hull_db)", info.Database)
	}
	if info.ComposeFile != "docker-compose.yml" {
		t.Errorf("compose file = %s", info.ComposeFile)
	}
}

func TestDetectLegacyPlain(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "compose.yaml", `services:
  app:
    image: serversideup/php:8.4-fpm-nginx
    environment:
      WEB_DOCUMENT_ROOT: /var/www/html
      NGINX_WEBROOT: /var/www/html
    labels:
      caddy: api.test
`)
	info, err := DetectLegacy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Template != "plain" || info.PHP != "" {
		t.Errorf("info = %+v", info)
	}
}

func TestDetectLegacyRejectsUnknown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "compose.yaml", "services:\n  web:\n    image: nginx:latest\n")
	if _, err := DetectLegacy(dir); err == nil {
		t.Error("unrecognized compose should error")
	}
}
