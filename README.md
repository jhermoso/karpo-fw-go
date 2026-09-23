# Karpo.Fw.Go

[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/jhermoso/karpo-fw-go)](https://goreportcard.com/report/github.com/jhermoso/karpo-fw-go)

Framework base en Go para la plataforma **Karpo**.

Diseñado bajo principios de **Clean Architecture**, **Inversión de Dependencias (DIP)** y **bajo consumo de recursos** (< 30 MB por servicio), optimizado para ser la base de microservicios de dominio (slices/publishers) e interactuar con herramientas de generación y Low-Code asistidas por LLMs.

---

## 🏛️ Filosofía y Arquitectura

1. **Aislamiento por Paquete (sin sobrecarga de proyectos)**:
   A diferencia del ecosistema tradicional .NET (donde cada adaptador requería un `.csproj` independiente), en Go el compilador prohíbe ciclos de importación y compila únicamente los paquetes efectivamente utilizados. Cada vertical del framework expone sus contratos e implementaciones en paquetes aislados.
2. **Result Pattern en lugar de excepciones**:
   Toda operación con posibilidad de fallo se modela de forma determinista mediante `result.Result[T]`, evitando costes de desenrollado de pila (*stack unwinding*) y garantizando seguridad en tiempo de compilación.
3. **Determinismo y Testabilidad**:
   Contratos puros para tiempo (`time.Clock`) y registro (`log.Logger`), permitiendo pruebas unitarias e integración 100% deterministas (relojes congelables, captura de trazas sin I/O real).
4. **Orientación a Grafos (futura persistencia)**:
   Base preparada para la integración con **ent** (ORM orientado a grafos) y la definición de modelos interpretables por agentes de IA.

---

## 📦 Estructura del Framework

```
karpo-fw-go/
├── LICENSE              # GNU General Public License v2.0
├── README.md            # Documentación general
├── go.mod               # Módulo github.com/jhermoso/karpo-fw-go
├── pkg/
│   ├── result/          # Tipado funcional Result[T], Ok, Fail, Map, FlatMap
│   ├── time/            # Contrato Clock y adaptadores (real y fake/mock)
│   │   ├── real/        # Reloj basado en stdlib time.Now()
│   │   └── fake/        # Reloj congelable/controlable para pruebas
│   ├── log/             # Contrato Logger estructurado
│   │   └── vanilla/     # Adaptador basado en standard library log/slog
│   └── persistence/     # (Próximo) Contratos de repositorio y adaptador ent
```

---

## 🚀 Requisitos y Uso

### Requisitos
- **Go 1.23+** (probado con Go 1.27.0).

### Compilar y Probar
```powershell
# Ejecutar todas las pruebas unitarias
go test ./... -v

# Verificar cobertura de código
go test ./... -cover
```

---

## 📄 Licencia

Este proyecto está licenciado bajo la **GNU General Public License v2.0** (GPL-2.0), la misma licencia que el kernel de Linux. Consulta el archivo [LICENSE](LICENSE) para más detalles.
