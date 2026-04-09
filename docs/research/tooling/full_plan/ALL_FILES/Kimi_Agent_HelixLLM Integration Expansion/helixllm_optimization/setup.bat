@echo off
REM ============================================================================
REM HelixLLM Master Setup Script for Windows
REM Complete installation and configuration for optimal performance
REM ============================================================================

echo ===============================================
echo          HelixLLM Master Setup (Windows)
echo ===============================================
echo.

set SCRIPT_DIR=%~dp0
cd /d "%SCRIPT_DIR%"

REM Check for Python
echo Checking Python installation...
python --version >nul 2>&1
if errorlevel 1 (
    echo Error: Python not found. Please install Python 3.10+ from https://python.org
    exit /b 1
)
echo [OK] Python found

REM Check for CUDA
echo.
echo Checking CUDA installation...
nvcc --version >nul 2>&1
if errorlevel 1 (
    echo Warning: CUDA not found. Please install CUDA 12.1 from https://developer.nvidia.com/cuda-downloads
    echo Continuing with CPU-only mode...
) else (
    echo [OK] CUDA found
    nvcc --version
)

REM Create virtual environment
echo.
echo Creating virtual environment...
if exist "%USERPROFILE%\helixllm_env" (
    echo Virtual environment already exists
) else (
    python -m venv "%USERPROFILE%\helixllm_env"
    echo [OK] Virtual environment created
)

REM Activate virtual environment
echo.
echo Activating virtual environment...
call "%USERPROFILE%\helixllm_env\Scripts\activate.bat"
echo [OK] Virtual environment activated

REM Upgrade pip
echo.
echo Upgrading pip...
python -m pip install --upgrade pip setuptools wheel
echo [OK] pip upgraded

REM Install dependencies
echo.
echo Installing dependencies...
pip install numpy psutil tqdm

REM Install llama-cpp-python with CUDA
echo.
echo Installing llama-cpp-python...
echo This may take several minutes...

set CMAKE_ARGS=-DLLAMA_CUDA=on -DLLAMA_CUDA_F16=on -DLLAMA_NATIVE=on
pip install llama-cpp-python --no-cache-dir --force-reinstall

if errorlevel 1 (
    echo Warning: CUDA installation failed, trying CPU-only...
    pip install llama-cpp-python --no-cache-dir --force-reinstall
)

echo [OK] llama-cpp-python installed

REM Create directories
echo.
echo Creating directories...
if not exist "models" mkdir models
if not exist "%USERPROFILE%\.config\helixllm" mkdir "%USERPROFILE%\.config\helixllm"
echo [OK] Directories created

REM Create environment configuration
echo.
echo Creating environment configuration...
(
echo @echo off
echo REM HelixLLM Environment Configuration
echo.
echo REM CUDA
echo set CUDA_HOME=C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.1
echo set PATH=%%CUDA_HOME%%\bin;%%PATH%%
echo.
echo REM Performance
echo set LLAMA_CUDA_FORCE_MMQ=1
echo set LLAMA_CUDA_F16=1
echo set OMP_NUM_THREADS=16
echo.
echo REM Python
echo set PYTHONUNBUFFERED=1
echo set PYTHONDONTWRITEBYTECODE=1
) > "%USERPROFILE%\.config\helixllm\environment.bat"

echo [OK] Environment configuration created

REM Run hardware detection
echo.
echo Running hardware detection...
if exist "06_hardware_detection.py" (
    python 06_hardware_detection.py
) else (
    echo Warning: Hardware detection script not found
)

REM Download models prompt
echo.
echo ===============================================
echo Model Download
echo ===============================================
echo.
echo Recommended models for 6GB VRAM:
echo   - Qwen2.5-1.5B-Instruct-Q4_K_M.gguf (~1GB)
echo   - nomic-embed-text-v1.5.Q4_K_M.gguf (~300MB)
echo.
set /p DOWNLOAD_MODELS="Download recommended models now? (y/n): "

if /i "%DOWNLOAD_MODELS%"=="y" (
    if exist "11_download_models.py" (
        python 11_download_models.py --download-all
    ) else (
        echo Warning: Model download script not found
        echo Please download models manually from HuggingFace
    )
) else (
    echo Skipping model download.
    echo Run 'python 11_download_models.py' later to download.
)

REM Setup complete
echo.
echo ===============================================
echo          Setup Complete!
echo ===============================================
echo.
echo Next steps:
echo.
echo 1. Activate the virtual environment:
echo    %USERPROFILE%\helixllm_env\Scripts\activate.bat
echo.
echo 2. Load environment configuration:
echo    %USERPROFILE%\.config\helixllm\environment.bat
echo.
echo 3. Start the HelixLLM server:
echo    python 10_helixllm_server.py
echo.
echo 4. Or run a quick test:
echo    python 04_model_loader.py
echo.
echo Useful commands:
echo   - List models: python 11_download_models.py --list
echo   - Run benchmark: python 09_benchmark.py
echo.
echo Happy inferencing!
echo.

pause
