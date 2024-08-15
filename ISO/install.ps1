$installUrl = "https://github.com/aubreyrs/Configs/releases/download/Testing/install.exe"
$configUrl = "https://github.com/aubreyrs/Configs/releases/download/Testing/config.yml"

$installPath = "$env:TEMP\install.exe"
$configPath = "$env:TEMP\config.yml"
Invoke-WebRequest -Uri $installUrl -OutFile $installPath
Invoke-WebRequest -Uri $configUrl -OutFile $configPath
Start-Process -FilePath $installPath -ArgumentList "-config $configPath" -Verb RunAs
