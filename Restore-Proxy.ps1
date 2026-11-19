$ErrorActionPreference='Stop'
& (Join-Path $PSScriptRoot 'scripts\integrations.ps1') -Action restore -Root $PSScriptRoot
if($LASTEXITCODE-ne0){throw 'Some settings could not be restored. Check the message above.'}
