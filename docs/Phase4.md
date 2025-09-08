Phase 5: Refactoring for Modularity & Optimization
This document outlines the key architectural change in Phase 5: separating the animation logic from the rendering logic.

Before: Tightly Coupled Logic
Before this phase, the MatrixEngine was directly responsible for both calculating the animation state and writing characters to the terminal. The updateAndRender method performed both tasks, making the engine and screen tightly coupled.

graph TD
    A[main() loop] --> B[MatrixEngine];
    B --> C[updateAndRender()];
    C --> D(Accesses terminal directly);
    D --> E[Screen];

After: Decoupled Components
In this phase, we introduced a Frame object to act as an intermediary. The MatrixEngine now only generates a new Frame object (a data snapshot of the screen). The Screen then takes this Frame and handles the low-level rendering. This separates the animation logic from the display logic, improving modularity and maintainability.

graph TD
    A[main() loop] --> B[MatrixEngine];
    B --> C[generateFrame()];
    C --> D[Frame data object];
    D --> E[Screen];
    E --> F[renderFrame(Frame)];
    F --> G(Writes to terminal);
