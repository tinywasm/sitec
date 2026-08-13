# sitec

Compilador de sitio: toma un árbol de fuentes Go y produce la superficie
estática desplegable del sitio — hoja de estilos, bundle de scripts, sprite SVG,
declaración de fuentes y shell HTML.

Corre hasta terminar y sale. Es un compilador, no un servidor ni un
renderizador — pensado para CI/CD tanto como para el arnés de desarrollo.

```
sitec              # ayuda, exit 0
sitec build -o dir # compila y escribe la salida
sitec check        # valida sin escribir nada (puerta de CI)
```

stdout entrega datos (manifiesto JSON); stderr entrega logs.

## Estado

En construcción. Plan: [docs/PLAN.md](docs/PLAN.md).
