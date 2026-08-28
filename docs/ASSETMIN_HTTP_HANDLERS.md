# HTTP Handlers

`assetmin` provides a built-in HTTP handler to serve the bundled assets.

## Registration

```go
am := assetmin.NewAssetMin(config)
// Registers routes on the provided ServeMux
am.RegisterRoutes(myMux)
```

## Routes

- `/`: Serves `index.html`.
- `/{AssetsURLPrefix}/style.css`: Serves bundled CSS.
- `/{AssetsURLPrefix}/script.js`: Serves bundled JS.
- `/{AssetsURLPrefix}/favicon.svg`: Serves favicon.

Las rutas de los activos principales (`script.js`, `style.css`, `icons.svg`, `favicon.svg`) se guardan relativas cuando el build no declara páginas (shell WASM). La función `Read` de `AssetMin` resuelve estas rutas relativas contra la clave absoluta de la petición HTTP recibida (por ejemplo, `/script.js`).

## Caching
Assets are served with `ETag` and `Cache-Control` headers. If an asset changes, the ETag updates automatically.
