$ErrorActionPreference = "Continue"

$promoDir = $PSScriptRoot
$repoRoot = Resolve-Path (Join-Path $promoDir "..\..")
$frameSource = Join-Path $promoDir "promo-frames.html"
$frameDir = Join-Path $promoDir "frames"
$sceneDir = Join-Path $promoDir "scenes"
$outputPath = Join-Path $promoDir "msc-ambassador-promo.mp4"
$silentVideo = Join-Path $promoDir "promo-silent.mp4"
$voicePath = Join-Path $promoDir "voiceover.wav"
$musicPath = Join-Path $promoDir "music.wav"

New-Item -ItemType Directory -Path $frameDir, $sceneDir -Force | Out-Null

$chromeCandidates = @(
  "$env:ProgramFiles\Google\Chrome\Application\chrome.exe",
  "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
  "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe",
  "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe",
  "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe"
)
$chrome = $chromeCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $chrome) {
  throw "Chrome or Edge is required to compose the promo frames."
}

$toolDir = Join-Path $env:TEMP "msc-promo-tools"
$ffmpeg = Join-Path $toolDir "node_modules\ffmpeg-static\ffmpeg.exe"
if (-not (Test-Path $ffmpeg)) {
  New-Item -ItemType Directory -Path $toolDir -Force | Out-Null
  & npm install --prefix $toolDir ffmpeg-static@5.2.0 --no-audit --no-fund
}
if (-not (Test-Path $ffmpeg)) {
  throw "Unable to install the local FFmpeg encoder."
}

$scenes = @(
  [pscustomobject]@{ Name = "intro";       Duration = 5.5 },
  [pscustomobject]@{ Name = "index";       Duration = 6.5 },
  [pscustomobject]@{ Name = "program";     Duration = 7.0 },
  [pscustomobject]@{ Name = "rewards";     Duration = 7.0 },
  [pscustomobject]@{ Name = "referrals";   Duration = 6.0 },
  [pscustomobject]@{ Name = "nft";         Duration = 5.5 },
  [pscustomobject]@{ Name = "apply";       Duration = 6.0 },
  [pscustomobject]@{ Name = "leaderboard"; Duration = 5.5 },
  [pscustomobject]@{ Name = "outro";       Duration = 5.0 }
)

$frameUri = [System.Uri]::new($frameSource).AbsoluteUri
foreach ($scene in $scenes) {
  $framePath = Join-Path $frameDir "$($scene.Name).png"
  & $chrome `
    --headless `
    --disable-gpu `
    --hide-scrollbars `
    --allow-file-access-from-files `
    --window-size=1920,1080 `
    --force-device-scale-factor=1 `
    --virtual-time-budget=1500 `
    "--screenshot=$framePath" `
    "$frameUri`?scene=$($scene.Name)" 2>$null | Out-Null

  if (-not (Test-Path $framePath)) {
    throw "Frame capture failed for $($scene.Name)."
  }
}

foreach ($scene in $scenes) {
  $framePath = Join-Path $frameDir "$($scene.Name).png"
  $scenePath = Join-Path $sceneDir "$($scene.Name).mp4"
  $zoomStep = if ($scene.Name -in @("intro", "outro")) { "0.00005" } else { "0.00012" }
  $videoFilter = "zoompan=z='min(max(zoom,pzoom)+$zoomStep,1.035)':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':d=1:s=1920x1080:fps=30,format=yuv420p"

  & $ffmpeg -y `
    -framerate 30 `
    -loop 1 `
    -i $framePath `
    -t $scene.Duration `
    -vf $videoFilter `
    -r 30 `
    -an `
    -c:v libx264 `
    -preset medium `
    -crf 18 `
    -pix_fmt yuv420p `
    $scenePath

  if ($LASTEXITCODE -ne 0) {
    throw "Video scene render failed for $($scene.Name)."
  }
}

$transitionDuration = 0.65
$inputArgs = @()
foreach ($scene in $scenes) {
  $inputArgs += @("-i", (Join-Path $sceneDir "$($scene.Name).mp4"))
}

$filterParts = @()
$runningDuration = [double]$scenes[0].Duration
$previousLabel = "0:v"
for ($index = 1; $index -lt $scenes.Count; $index++) {
  $offset = $runningDuration - $transitionDuration
  $outputLabel = "v$index"
  $filterParts += "[$previousLabel][$index`:v]xfade=transition=fade:duration=$transitionDuration`:offset=$($offset.ToString('0.00', [Globalization.CultureInfo]::InvariantCulture))[$outputLabel]"
  $previousLabel = $outputLabel
  $runningDuration += [double]$scenes[$index].Duration - $transitionDuration
}
$videoDuration = $runningDuration
$filterComplex = $filterParts -join ";"

& $ffmpeg -y `
  @inputArgs `
  -filter_complex $filterComplex `
  -map "[$previousLabel]" `
  -an `
  -c:v libx264 `
  -preset medium `
  -crf 18 `
  -pix_fmt yuv420p `
  -movflags +faststart `
  $silentVideo
if ($LASTEXITCODE -ne 0) {
  throw "Scene transition render failed."
}

Add-Type -AssemblyName System.Speech
$speaker = New-Object System.Speech.Synthesis.SpeechSynthesizer
try {
  if ($speaker.GetInstalledVoices().VoiceInfo.Name -contains "Microsoft Zira Desktop") {
    $speaker.SelectVoice("Microsoft Zira Desktop")
  }
  $speaker.Rate = 4
  $speaker.Volume = 100
  $speaker.SetOutputToWaveFile($voicePath)
  $speaker.Speak((Get-Content (Join-Path $promoDir "narration.txt") -Raw))
}
finally {
  $speaker.Dispose()
}

$durationText = $videoDuration.ToString("0.00", [Globalization.CultureInfo]::InvariantCulture)
& $ffmpeg -y `
  -f lavfi -i "sine=frequency=55:sample_rate=48000:duration=$durationText" `
  -f lavfi -i "sine=frequency=110:sample_rate=48000:duration=$durationText" `
  -f lavfi -i "sine=frequency=220:sample_rate=48000:duration=$durationText" `
  -filter_complex "[0:a]volume=0.080,tremolo=f=2:d=0.65[bass];[1:a]volume=0.030,tremolo=f=0.25:d=0.35[mid];[2:a]volume=0.012,tremolo=f=0.125:d=0.25[high];[bass][mid][high]amix=inputs=3:duration=longest:normalize=0,afade=t=in:st=0:d=1.5,afade=t=out:st=$($videoDuration - 2.0):d=2.0[music]" `
  -map "[music]" `
  -c:a pcm_s16le `
  $musicPath
if ($LASTEXITCODE -ne 0) {
  throw "Music generation failed."
}

& $ffmpeg -y `
  -i $silentVideo `
  -i $voicePath `
  -i $musicPath `
  -filter_complex "[1:a]adelay=650|650,volume=1.10,apad=whole_dur=$durationText[voice];[2:a]volume=0.75[bed];[bed][voice]amix=inputs=2:duration=first:normalize=0,alimiter=limit=0.95[audio]" `
  -map 0:v `
  -map "[audio]" `
  -c:v copy `
  -c:a aac `
  -b:a 192k `
  -movflags +faststart `
  -t $durationText `
  $outputPath
if ($LASTEXITCODE -ne 0) {
  throw "Final promo render failed."
}

Write-Host "Promo video created: $outputPath"
