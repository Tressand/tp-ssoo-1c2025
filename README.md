## NOTA PARA EL DOCENTE
Se agregó un programa que buildea automaticamente los cuatro módulos y coloca los archivos de configuración en la disposición correcta.
Desde la carpeta raiz del repositorio, ejecutar este comando:
`./build_all.exe`
Este programa compila los cuatro módulos y coloca sus respectivos archivos de configuración dentro de la carpeta "builds/". El codigo fuente de este programa utilitario se encuentra en "utils/build_all/build_all.go"

## Checkpoint

Para cada checkpoint de control obligatorio, se debe crear un tag en el
repositorio con el siguiente formato:

```
checkpoint-{número}
```

Donde `{número}` es el número del checkpoint.

Para crear un tag y subirlo al repositorio, podemos utilizar los siguientes
comandos:

```bash
git tag -a checkpoint-{número} -m "Checkpoint {número}"
git push origin checkpoint-{número}
```

Asegúrense de que el código compila y cumple con los requisitos del checkpoint
antes de subir el tag.
