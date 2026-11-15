# Pester 5 tests of scripts/install.ps1 (PLT-F-043). CI only (windows-latest, PowerShell 7 and Windows
# PowerShell 5.1): `Invoke-Pester scripts/test -CI`.
#
# The script is dot-sourced with LOCAL_SERVER_INSTALL_NO_MAIN set, so only its functions are defined.
# Nothing here touches the real user PATH, the registry, the network or a real installer: the two
# registry functions, curl, the service program and the Ollama and Tailscale checks are mocked, and
# every file goes to $TestDrive. The tests that need a question answered use the answers file of
# LOCAL_SERVER_TEST_ANSWERS, so they never wait for a keyboard.

BeforeAll {
    $env:LOCAL_SERVER_INSTALL_NO_MAIN = '1'
    . (Join-Path $PSScriptRoot '..\install.ps1')
}

AfterAll {
    Remove-Item Env:\LOCAL_SERVER_INSTALL_NO_MAIN -ErrorAction SilentlyContinue
}

Describe 'architecture' {
    It 'maps <Name> to <Expected>' -ForEach @(
        @{ Name = 'X64'; Expected = 'amd64' },
        @{ Name = 'Arm64'; Expected = 'arm64' }
    ) {
        ConvertTo-LsArch -Name $Name | Should -Be $Expected
    }

    It 'refuses <Name>' -ForEach @(
        @{ Name = 'X86' },
        @{ Name = 'Arm' },
        @{ Name = 'Wasm' },
        @{ Name = '' }
    ) {
        { ConvertTo-LsArch -Name $Name } | Should -Throw '*unsupported CPU type*'
    }

    It 'reads the CPU of this machine as a name the mapping knows or refuses cleanly' {
        $name = Get-LsOsArchitecture
        $name | Should -Not -BeNullOrEmpty
    }
}

Describe 'folders' {
    BeforeEach {
        $script:savedPrefix = $env:LOCAL_SERVER_PREFIX
        $script:savedData = $env:LS_DATA_DIR
        $script:savedAppData = $env:APPDATA
        $script:savedLocal = $env:LOCALAPPDATA
        Remove-Item Env:\LOCAL_SERVER_PREFIX -ErrorAction SilentlyContinue
        Remove-Item Env:\LS_DATA_DIR -ErrorAction SilentlyContinue
        $env:APPDATA = 'C:\Users\test\AppData\Roaming'
        $env:LOCALAPPDATA = 'C:\Users\test\AppData\Local'
    }
    AfterEach {
        $env:LOCAL_SERVER_PREFIX = $script:savedPrefix
        $env:LS_DATA_DIR = $script:savedData
        $env:APPDATA = $script:savedAppData
        $env:LOCALAPPDATA = $script:savedLocal
    }

    It 'puts the data folder in %AppData%\local-server by default' {
        Get-LsDataDir | Should -Be 'C:\Users\test\AppData\Roaming\local-server'
    }

    It 'uses LS_DATA_DIR when it is set' {
        $env:LS_DATA_DIR = 'D:\my data'
        Get-LsDataDir | Should -Be 'D:\my data'
    }

    It 'installs into %LOCALAPPDATA%\Programs\local-server by default' {
        Get-LsInstallDir | Should -Be 'C:\Users\test\AppData\Local\Programs\local-server'
    }

    It 'uses LOCAL_SERVER_PREFIX when it is set' {
        $env:LOCAL_SERVER_PREFIX = 'D:\tools\ls\'
        Get-LsInstallDir | Should -Be 'D:\tools\ls'
    }
}

Describe 'release URLs' {
    BeforeEach {
        $script:savedBase = $env:LOCAL_SERVER_RELEASE_BASE
        Remove-Item Env:\LOCAL_SERVER_RELEASE_BASE -ErrorAction SilentlyContinue
    }
    AfterEach {
        $env:LOCAL_SERVER_RELEASE_BASE = $script:savedBase
    }

    It 'uses the latest release without a version' {
        Get-LsReleaseBase -Version '' | Should -Be 'https://github.com/inCoderKaka/local-server/releases/latest/download'
    }

    It 'uses the tag when a version is given' {
        Get-LsReleaseBase -Version 'v0.1.0-rc.1' | Should -Be 'https://github.com/inCoderKaka/local-server/releases/download/v0.1.0-rc.1'
    }

    It 'lets LOCAL_SERVER_RELEASE_BASE win' {
        $env:LOCAL_SERVER_RELEASE_BASE = 'file:///C:/fake/release/'
        Get-LsReleaseBase -Version 'v9' | Should -Be 'file:///C:/fake/release'
    }
}

Describe 'checksum' {
    BeforeAll {
        $script:good = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
    }

    It 'finds the line of the asset among others' {
        $sums = "$($script:good)  local-server_windows_arm64.exe`n" + ('f' * 64) + "  local-server_windows_amd64.exe`n"
        Get-LsExpectedHash -SumsText $sums -Name 'local-server_windows_amd64.exe' | Should -Be ('f' * 64)
        Get-LsExpectedHash -SumsText $sums -Name 'local-server_windows_arm64.exe' | Should -Be $script:good
    }

    It 'reads lines with Windows line ends and a binary-mode star' {
        $sums = "$($script:good) *local-server_windows_amd64.exe`r`n"
        Get-LsExpectedHash -SumsText $sums -Name 'local-server_windows_amd64.exe' | Should -Be $script:good
    }

    It 'returns nothing for a name that has no line or a prefix of one' {
        $sums = "$($script:good)  local-server_windows_amd64.exe`n"
        Get-LsExpectedHash -SumsText $sums -Name 'local-server_windows_arm64.exe' | Should -BeNullOrEmpty
        Get-LsExpectedHash -SumsText $sums -Name 'local-server_windows_amd64' | Should -BeNullOrEmpty
    }

    It 'compares hashes without regard to case' {
        Test-LsHashEqual -Expected 'ABCDEF' -Actual 'abcdef' | Should -BeTrue
        Test-LsHashEqual -Expected 'abcdef' -Actual 'abcdee' | Should -BeFalse
    }

    Context 'against a real file' {
        BeforeEach {
            $script:file = Join-Path $TestDrive 'local-server_windows_amd64.exe'
            $script:sumsFile = Join-Path $TestDrive 'SHA256SUMS'
            [System.IO.File]::WriteAllText($script:file, 'pretend binary')
            $script:actual = (Get-FileHash -LiteralPath $script:file -Algorithm SHA256).Hash
        }

        It 'accepts a matching line, in either case' {
            [System.IO.File]::WriteAllText($script:sumsFile, "$($script:actual.ToLowerInvariant())  local-server_windows_amd64.exe`n")
            { Assert-LsChecksum -FilePath $script:file -Name 'local-server_windows_amd64.exe' -SumsPath $script:sumsFile } | Should -Not -Throw
            [System.IO.File]::WriteAllText($script:sumsFile, "$($script:actual.ToUpperInvariant())  local-server_windows_amd64.exe`n")
            { Assert-LsChecksum -FilePath $script:file -Name 'local-server_windows_amd64.exe' -SumsPath $script:sumsFile } | Should -Not -Throw
        }

        It 'stops on a mismatch' {
            [System.IO.File]::WriteAllText($script:sumsFile, ('1' * 64) + "  local-server_windows_amd64.exe`n")
            { Assert-LsChecksum -FilePath $script:file -Name 'local-server_windows_amd64.exe' -SumsPath $script:sumsFile } |
                Should -Throw '*does not match*Nothing was installed*'
        }

        It 'stops when the line is missing' {
            [System.IO.File]::WriteAllText($script:sumsFile, ('1' * 64) + "  local-server_windows_arm64.exe`n")
            { Assert-LsChecksum -FilePath $script:file -Name 'local-server_windows_amd64.exe' -SumsPath $script:sumsFile } |
                Should -Throw '*has no line for local-server_windows_amd64.exe*'
        }
    }
}

Describe 'questions' {
    BeforeEach {
        Reset-LsState
        $script:savedAnswers = $env:LOCAL_SERVER_TEST_ANSWERS
        $script:answersFile = Join-Path $TestDrive 'answers.txt'
        $env:LOCAL_SERVER_TEST_ANSWERS = $script:answersFile
    }
    AfterEach {
        $env:LOCAL_SERVER_TEST_ANSWERS = $script:savedAnswers
    }

    It 'takes the answers from the file in order: yes, no, yes' {
        Set-Content -LiteralPath $script:answersFile -Value @('y', 'n', 'YES')
        Request-LsConsent -Prompt 'one?' | Should -BeTrue
        Request-LsConsent -Prompt 'two?' | Should -BeFalse
        Request-LsConsent -Prompt 'three?' | Should -BeTrue
    }

    It 'counts anything but yes as no' {
        Set-Content -LiteralPath $script:answersFile -Value @('', 'maybe', 'yess')
        Request-LsConsent -Prompt 'one?' | Should -BeFalse
        Request-LsConsent -Prompt 'two?' | Should -BeFalse
        Request-LsConsent -Prompt 'three?' | Should -BeFalse
    }

    It 'answers no once the file is used up' {
        Set-Content -LiteralPath $script:answersFile -Value @('y')
        Request-LsConsent -Prompt 'one?' | Should -BeTrue
        Request-LsConsent -Prompt 'two?' | Should -BeFalse
        Request-LsConsent -Prompt 'three?' | Should -BeFalse
    }

    It 'answers no when the file does not exist' {
        Request-LsConsent -Prompt 'one?' | Should -BeFalse
    }

    It 'with no hook and no keyboard answers no and keeps the manual steps for the end' {
        Remove-Item Env:\LOCAL_SERVER_TEST_ANSWERS
        Mock Test-LsInteractive { return $false }
        Mock Read-LsHostLine { return 'y' }
        Request-LsConsent -Prompt 'one?' | Should -BeFalse
        Should -Invoke Read-LsHostLine -Times 0
        Resolve-LsDecline -Key 'ollama'
        @(Get-LsSkippedKeys) | Should -Be @('ollama')
    }

    It 'with a keyboard asks it, and a plain no shows the steps now instead of keeping them' {
        Remove-Item Env:\LOCAL_SERVER_TEST_ANSWERS
        Mock Test-LsInteractive { return $true }
        Mock Read-LsHostLine { return 'n' }
        Mock Show-LsManualSteps { }
        Request-LsConsent -Prompt 'one?' | Should -BeFalse
        Resolve-LsDecline -Key 'ollama'
        @(Get-LsSkippedKeys).Count | Should -Be 0
        Should -Invoke Show-LsManualSteps -Times 1 -ParameterFilter { $Key -eq 'ollama' }
    }

    It 'a keyboard that says yes gives yes' {
        Remove-Item Env:\LOCAL_SERVER_TEST_ANSWERS
        Mock Test-LsInteractive { return $true }
        Mock Read-LsHostLine { return 'y' }
        Request-LsConsent -Prompt 'one?' | Should -BeTrue
    }
}

Describe 'user PATH strings' {
    It 'adds an entry' {
        Add-LsPathEntry -PathValue 'C:\a;C:\b' -Dir 'C:\ls' | Should -Be 'C:\a;C:\b;C:\ls'
    }

    It 'adds to an empty PATH without a leading semicolon' {
        Add-LsPathEntry -PathValue '' -Dir 'C:\ls' | Should -Be 'C:\ls'
    }

    It 'adds nothing twice, whatever the case or a trailing backslash' {
        $once = Add-LsPathEntry -PathValue 'C:\a' -Dir 'C:\Users\Me\ls'
        Add-LsPathEntry -PathValue $once -Dir 'C:\Users\Me\ls' | Should -Be $once
        Add-LsPathEntry -PathValue $once -Dir 'c:\users\me\LS\' | Should -Be $once
    }

    It 'does not double a semicolon at the end' {
        Add-LsPathEntry -PathValue 'C:\a;' -Dir 'C:\ls' | Should -Be 'C:\a;C:\ls'
    }

    It 'removes only that entry, in any case, and keeps the others in order' {
        Remove-LsPathEntry -PathValue 'C:\a;C:\Users\Me\ls\;C:\b' -Dir 'c:\users\me\ls' | Should -Be 'C:\a;C:\b'
    }

    It 'removes the entry when it is the only one' {
        Remove-LsPathEntry -PathValue 'C:\ls' -Dir 'C:\ls' | Should -Be ''
    }

    It 'leaves a PATH without the entry as it was' {
        Remove-LsPathEntry -PathValue 'C:\a;C:\b' -Dir 'C:\ls' | Should -Be 'C:\a;C:\b'
    }

    It 'adding and then removing gives back the PATH' {
        $start = 'C:\a;C:\b'
        Remove-LsPathEntry -PathValue (Add-LsPathEntry -PathValue $start -Dir 'C:\ls') -Dir 'C:\ls' | Should -Be $start
    }

    It 'knows whether an entry is there' {
        Test-LsPathHasEntry -PathValue 'C:\a;C:\LS' -Dir 'c:\ls\' | Should -BeTrue
        Test-LsPathHasEntry -PathValue 'C:\a;C:\lsx' -Dir 'C:\ls' | Should -BeFalse
        Test-LsPathHasEntry -PathValue '' -Dir 'C:\ls' | Should -BeFalse
    }
}

Describe 'other pure helpers' {
    It 'reads the service status text' {
        $running = ConvertFrom-LsServiceStatus -Text "local-server background service, user mode: installed, running`n"
        $running.UserInstalled | Should -BeTrue
        $running.UserRunning | Should -BeTrue
        $running.SystemInstalled | Should -BeFalse

        $stopped = ConvertFrom-LsServiceStatus -Text "local-server background service, user mode: installed, stopped`n"
        $stopped.UserInstalled | Should -BeTrue
        $stopped.UserRunning | Should -BeFalse

        $system = ConvertFrom-LsServiceStatus -Text "local-server background service, system mode: installed, running`n"
        $system.UserInstalled | Should -BeFalse
        $system.SystemInstalled | Should -BeTrue

        $none = ConvertFrom-LsServiceStatus -Text 'local-server background service: not installed (run `local-server service install`)'
        $none.UserInstalled | Should -BeFalse
        $none.SystemInstalled | Should -BeFalse
    }

    It 'builds the Ollama address from OLLAMA_HOST' {
        $saved = $env:OLLAMA_HOST
        try {
            Remove-Item Env:\OLLAMA_HOST -ErrorAction SilentlyContinue
            Get-LsOllamaUrl | Should -Be 'http://127.0.0.1:11434'
            $env:OLLAMA_HOST = '127.0.0.1:9999'
            Get-LsOllamaUrl | Should -Be 'http://127.0.0.1:9999'
            $env:OLLAMA_HOST = 'https://ollama.example:443/'
            Get-LsOllamaUrl | Should -Be 'https://ollama.example:443'
            $env:OLLAMA_HOST = '0.0.0.0:11434'
            Get-LsOllamaUrl | Should -Be 'http://127.0.0.1:11434'
        } finally {
            $env:OLLAMA_HOST = $saved
        }
    }
}

Describe 'the whole script with the outside world mocked' {
    BeforeEach {
        Reset-LsState
        $script:saved = @{}
        $script:savedPath = $env:Path
        foreach ($n in @('LOCAL_SERVER_PREFIX', 'LOCAL_SERVER_RELEASE_BASE', 'LOCAL_SERVER_TEST_ANSWERS', 'LOCAL_SERVER_ACTION', 'LOCAL_SERVER_VERSION', 'LS_DATA_DIR', 'APPDATA', 'LOCAL_SERVER_OLLAMA_WAIT')) {
            $script:saved[$n] = [Environment]::GetEnvironmentVariable($n)
        }
        $script:prefix = Join-Path $TestDrive 'prefix'
        $script:release = Join-Path $TestDrive 'release'
        $script:data = Join-Path $TestDrive 'data'
        New-Item -ItemType Directory -Path $script:release | Out-Null
        $env:LOCAL_SERVER_PREFIX = $script:prefix
        $env:LOCAL_SERVER_RELEASE_BASE = 'https://example.invalid/release'
        $env:LOCAL_SERVER_TEST_ANSWERS = Join-Path $TestDrive 'answers.txt'
        $env:LS_DATA_DIR = $script:data
        $env:LOCAL_SERVER_OLLAMA_WAIT = '0'
        Remove-Item Env:\LOCAL_SERVER_ACTION -ErrorAction SilentlyContinue
        Remove-Item Env:\LOCAL_SERVER_VERSION -ErrorAction SilentlyContinue
        Set-Content -LiteralPath $env:LOCAL_SERVER_TEST_ANSWERS -Value @('n')

        # The fake release: a "binary" and a SHA256SUMS that matches it.
        $script:asset = 'local-server_windows_amd64.exe'
        [System.IO.File]::WriteAllText((Join-Path $script:release $script:asset), 'pretend binary')
        $hash = (Get-FileHash -LiteralPath (Join-Path $script:release $script:asset) -Algorithm SHA256).Hash.ToLowerInvariant()
        [System.IO.File]::WriteAllText((Join-Path $script:release 'SHA256SUMS'), "$hash  $($script:asset)`n")

        Mock Test-LsCurlPresent { return $true }
        Mock Get-LsOsArchitecture { return 'X64' }
        Mock Invoke-LsCurl {
            $name = ($Url -split '/')[-1]
            $src = Join-Path $script:release $name
            if (-not (Test-Path -LiteralPath $src)) { return 22 }
            Copy-Item -LiteralPath $src -Destination $OutFile
            return 0
        }
        Mock Get-LsUserPath { return 'C:\other' }
        Mock Set-LsUserPath { }
        Mock Test-LsOllamaInstalled { return $true }
        Mock Test-LsOllamaAnswers { return $false }
        Mock Test-LsTailscaleInstalled { return $true }
        Mock Get-LsProgramOutput { return [pscustomobject]@{ ExitCode = 0; Text = 'local-server v1.0.0' } }
        Mock Invoke-LsProgram { return 0 }
    }
    AfterEach {
        $env:Path = $script:savedPath
        foreach ($n in $script:saved.Keys) {
            [Environment]::SetEnvironmentVariable($n, $script:saved[$n])
        }
    }

    It 'installs the verified binary, registers the service and does not start it before a first run' {
        Install-LocalServerMain | Should -Be 0
        Test-Path -LiteralPath (Join-Path $script:prefix 'local-server.exe') | Should -BeTrue
        Get-Content -LiteralPath (Join-Path $script:prefix 'local-server.exe') -Raw | Should -Be 'pretend binary'
        Should -Invoke Invoke-LsProgram -Times 1 -ParameterFilter { ($ArgumentList -join ' ') -like 'service install*' }
        Should -Invoke Invoke-LsProgram -Times 0 -ParameterFilter { ($ArgumentList -join ' ') -like 'service start*' }
        Should -Invoke Invoke-LsProgram -Times 0 -ParameterFilter { $ArgumentList -contains 'serve' }
        # The data folder is made by serve, never by this script.
        Test-Path -LiteralPath $script:data | Should -BeFalse
        # PATH question answered no: the registry function was not called.
        Should -Invoke Set-LsUserPath -Times 0
    }

    It 'passes --data-dir to the service when LS_DATA_DIR is set' {
        Install-LocalServerMain | Should -Be 0
        Should -Invoke Invoke-LsProgram -Times 1 -ParameterFilter { ($ArgumentList -join ' ') -eq ('service install --data-dir ' + $script:data) }
    }

    It 'starts the service when a database already exists' {
        New-Item -ItemType Directory -Path $script:data | Out-Null
        [System.IO.File]::WriteAllText((Join-Path $script:data 'local-server.db'), '')
        Install-LocalServerMain | Should -Be 0
        Should -Invoke Invoke-LsProgram -Times 1 -ParameterFilter { ($ArgumentList -join ' ') -like 'service start*' }
    }

    It 'adds the install folder to the user PATH only after a yes' {
        Set-Content -LiteralPath $env:LOCAL_SERVER_TEST_ANSWERS -Value @('y')
        Mock Set-LsUserPath { }
        Install-LocalServerMain | Should -Be 0
        Should -Invoke Set-LsUserPath -Times 1 -ParameterFilter { $Value -eq ('C:\other;' + $script:prefix) }
    }

    It 'aborts on a checksum mismatch before anything is installed' {
        [System.IO.File]::WriteAllText((Join-Path $script:release 'SHA256SUMS'), ('1' * 64) + "  $($script:asset)`n")
        Install-LocalServerMain | Should -Be 1
        Test-Path -LiteralPath (Join-Path $script:prefix 'local-server.exe') | Should -BeFalse
        Test-Path -LiteralPath $script:prefix | Should -BeFalse
        Should -Invoke Invoke-LsProgram -Times 0
    }

    It 'aborts when the SHA256SUMS line is missing before anything is installed' {
        [System.IO.File]::WriteAllText((Join-Path $script:release 'SHA256SUMS'), ('1' * 64) + "  local-server_windows_arm64.exe`n")
        Install-LocalServerMain | Should -Be 1
        Test-Path -LiteralPath $script:prefix | Should -BeFalse
        Should -Invoke Invoke-LsProgram -Times 0
    }

    It 'points at LOCAL_SERVER_VERSION when the download is not found' {
        Remove-Item -LiteralPath (Join-Path $script:release $script:asset)
        Install-LocalServerMain | Should -Be 1
        Test-Path -LiteralPath $script:prefix | Should -BeFalse
    }

    It 'refuses an unsupported CPU' {
        Mock Get-LsOsArchitecture { return 'X86' }
        Install-LocalServerMain | Should -Be 1
        Test-Path -LiteralPath $script:prefix | Should -BeFalse
    }

    It 'refuses an action it does not know' {
        $env:LOCAL_SERVER_ACTION = 'frobnicate'
        Install-LocalServerMain | Should -Be 1
    }

    It 'refuses a version tag with odd characters' {
        $env:LOCAL_SERVER_VERSION = 'v1;calc'
        Install-LocalServerMain | Should -Be 1
    }

    It 'upgrade needs an installed program' {
        $env:LOCAL_SERVER_ACTION = 'upgrade'
        Install-LocalServerMain | Should -Be 1
        Test-Path -LiteralPath $script:prefix | Should -BeFalse
    }

    It 'upgrade with the same version changes nothing' {
        New-Item -ItemType Directory -Path $script:prefix | Out-Null
        [System.IO.File]::WriteAllText((Join-Path $script:prefix 'local-server.exe'), 'old binary')
        $env:LOCAL_SERVER_ACTION = 'upgrade'
        Install-LocalServerMain | Should -Be 0
        Get-Content -LiteralPath (Join-Path $script:prefix 'local-server.exe') -Raw | Should -Be 'old binary'
        Should -Invoke Invoke-LsProgram -Times 0
    }

    It 'upgrade with a new version stops the service, replaces the binary and starts the service' {
        New-Item -ItemType Directory -Path $script:prefix | Out-Null
        [System.IO.File]::WriteAllText((Join-Path $script:prefix 'local-server.exe'), 'old binary')
        New-Item -ItemType Directory -Path $script:data | Out-Null
        [System.IO.File]::WriteAllText((Join-Path $script:data 'local-server.db'), 'keep me')
        Mock Get-LsProgramOutput {
            if ($ArgumentList -contains '--version') {
                # The downloaded binary is the one in the temp folder.
                if ($FilePath -like '*local-server-install-*') { return [pscustomobject]@{ ExitCode = 0; Text = 'local-server v2.0.0' } }
                return [pscustomobject]@{ ExitCode = 0; Text = 'local-server v1.0.0' }
            }
            return [pscustomobject]@{ ExitCode = 0; Text = 'local-server background service, user mode: installed, running' }
        }
        $env:LOCAL_SERVER_ACTION = 'upgrade'
        Install-LocalServerMain | Should -Be 0
        Get-Content -LiteralPath (Join-Path $script:prefix 'local-server.exe') -Raw | Should -Be 'pretend binary'
        Should -Invoke Invoke-LsProgram -Times 1 -ParameterFilter { ($ArgumentList -join ' ') -eq 'service stop' }
        Should -Invoke Invoke-LsProgram -Times 1 -ParameterFilter { ($ArgumentList -join ' ') -eq 'service start' }
        Get-Content -LiteralPath (Join-Path $script:data 'local-server.db') -Raw | Should -Be 'keep me'
    }

    It 'uninstall removes the program, the service and the PATH entry, and keeps the data folder on a no' {
        New-Item -ItemType Directory -Path $script:prefix | Out-Null
        [System.IO.File]::WriteAllText((Join-Path $script:prefix 'local-server.exe'), 'old binary')
        New-Item -ItemType Directory -Path $script:data | Out-Null
        [System.IO.File]::WriteAllText((Join-Path $script:data 'local-server.db'), 'keep me')
        Mock Get-LsUserPath { return ('C:\other;' + $script:prefix) }
        Mock Get-LsProgramOutput { return [pscustomobject]@{ ExitCode = 0; Text = 'local-server background service, user mode: installed, running' } }
        $env:LOCAL_SERVER_ACTION = 'uninstall'
        Install-LocalServerMain | Should -Be 0
        Test-Path -LiteralPath (Join-Path $script:prefix 'local-server.exe') | Should -BeFalse
        Should -Invoke Invoke-LsProgram -Times 1 -ParameterFilter { ($ArgumentList -join ' ') -eq 'service stop' }
        Should -Invoke Invoke-LsProgram -Times 1 -ParameterFilter { ($ArgumentList -join ' ') -eq 'service uninstall' }
        Should -Invoke Set-LsUserPath -Times 1 -ParameterFilter { $Value -eq 'C:\other' }
        Test-Path -LiteralPath (Join-Path $script:data 'local-server.db') | Should -BeTrue
    }

    It 'uninstall deletes the data folder on a yes' {
        New-Item -ItemType Directory -Path $script:data | Out-Null
        [System.IO.File]::WriteAllText((Join-Path $script:data 'local-server.db'), 'x')
        Set-Content -LiteralPath $env:LOCAL_SERVER_TEST_ANSWERS -Value @('y')
        $env:LOCAL_SERVER_ACTION = 'uninstall'
        Install-LocalServerMain | Should -Be 0
        Test-Path -LiteralPath $script:data | Should -BeFalse
    }
}
