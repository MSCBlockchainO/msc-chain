$ErrorActionPreference = "Continue"

$promoDir = $PSScriptRoot
$frameSource = Join-Path $promoDir "social-frames.html"
$frameRoot = Join-Path $promoDir "frames"
$sceneRoot = Join-Path $promoDir "scenes"
$voicePath = Join-Path $promoDir "voiceover.wav"
$musicPath = Join-Path $promoDir "music.wav"

New-Item -ItemType Directory -Path $frameRoot, $sceneRoot -Force | Out-Null

$chromeCandidates = @(
  "$env:ProgramFiles\Google\Chrome\Application\chrome.exe",
  "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
  "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe",
  "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe",
  "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe"
)
$chrome = $chromeCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $chrome) {
  throw "Chrome or Edge is required to compose the social video frames."
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
  [pscustomobject]@{ Name = "hook";     Duration = 5.0 },
  [pscustomobject]@{ Name = "builders"; Duration = 4.5 },
  [pscustomobject]@{ Name = "gaming";   Duration = 4.5 },
  [pscustomobject]@{ Name = "program";  Duration = 5.0 },
  [pscustomobject]@{ Name = "rewards";  Duration = 5.0 },
  [pscustomobject]@{ Name = "ama";      Duration = 4.5 },
  [pscustomobject]@{ Name = "apply";    Duration = 4.5 },
  [pscustomobject]@{ Name = "outro";    Duration = 4.0 }
)

$formats = @(
  [pscustomobject]@{
    Name = "landscape"
    Width = 1920
    Height = 1080
    Output = "msc-ambassador-youtube-1080p.mp4"
  },
  [pscustomobject]@{
    Name = "portrait"
    Width = 1080
    Height = 1920
    Output = "msc-ambassador-instagram-reel.mp4"
  }
)

Add-Type -AssemblyName System.Speech
$speaker = New-Object System.Speech.Synthesis.SpeechSynthesizer
try {
  if ($speaker.GetInstalledVoices().VoiceInfo.Name -contains "Microsoft Zira Desktop") {
    $speaker.SelectVoice("Microsoft Zira Desktop")
  }
  $speaker.Rate = 5
  $speaker.Volume = 100
  $speaker.SetOutputToWaveFile($voicePath)
  $speaker.Speak((Get-Content (Join-Path $promoDir "narration.txt") -Raw))
}
finally {
  $speaker.Dispose()
}

$transitionDuration = 0.55
$videoDuration = [double]$scenes[0].Duration
for ($index = 1; $index -lt $scenes.Count; $index++) {
  $videoDuration += [double]$scenes[$index].Duration - $transitionDuration
}
$durationText = $videoDuration.ToString("0.00", [Globalization.CultureInfo]::InvariantCulture)

& $ffmpeg -y `
  -f lavfi -i "sine=frequency=55:sample_rate=48000:duration=$durationText" `
  -f lavfi -i "sine=frequency=110:sample_rate=48000:duration=$durationText" `
  -f lavfi -i "sine=frequency=220:sample_rate=48000:duration=$durationText" `
  -filter_complex "[0:a]volume=0.075,tremolo=f=2:d=0.68[bass];[1:a]volume=0.028,tremolo=f=0.25:d=0.35[mid];[2:a]volume=0.010,tremolo=f=0.125:d=0.25[high];[bass][mid][high]amix=inputs=3:duration=longest:normalize=0,afade=t=in:st=0:d=1.2,afade=t=out:st=$($videoDuration - 1.8):d=1.8[music]" `
  -map "[music]" `
  -c:a pcm_s16le `
  $musicPath
if ($LASTEXITCODE -ne 0) {
  throw "Music generation failed."
}

$frameUri = [System.Uri]::new($frameSource).AbsoluteUri

foreach ($format in $formats) {
  $frameDir = Join-Path $frameRoot $format.Name
  $sceneDir = Join-Path $sceneRoot $format.Name
  New-Item -ItemType Directory -Path $frameDir, $sceneDir -Force | Out-Null

  foreach ($scene in $scenes) {
    $framePath = Join-Path $frameDir "$($scene.Name).png"
    & $chrome `
      --headless `
      --disable-gpu `
      --hide-scrollbars `
      --allow-file-access-from-files `
      "--window-size=$($format.Width),$($format.Height)" `
      --force-device-scale-factor=1 `
      --virtual-time-budget=1500 `
      "--screenshot=$framePath" `
      "$frameUri`?scene=$($scene.Name)&format=$($format.Name)" 2>$null | Out-Null

    if (-not (Test-Path $framePath)) {
      throw "Frame capture failed for $($format.Name)/$($scene.Name)."
    }

    $scenePath = Join-Path $sceneDir "$($scene.Name).mp4"
    $zoomStep = if ($scene.Name -in @("program", "rewards", "apply", "outro")) { "0.00004" } else { "0.00010" }
    $videoFilter = "zoompan=z='min(max(zoom,pzoom)+$zoomStep,1.025)':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':d=1:s=$($format.Width)x$($format.Height):fps=30,format=yuv420p"

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
      throw "Scene render failed for $($format.Name)/$($scene.Name)."
    }
  }

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
    $offsetText = $offset.ToString("0.00", [Globalization.CultureInfo]::InvariantCulture)
    $filterParts += "[$previousLabel][$index`:v]xfade=transition=fade:duration=$transitionDuration`:offset=$offsetText[$outputLabel]"
    $previousLabel = $outputLabel
    $runningDuration += [double]$scenes[$index].Duration - $transitionDuration
  }

  $silentVideo = Join-Path $promoDir "$($format.Name)-silent.mp4"
  & $ffmpeg -y `
    @inputArgs `
    -filter_complex ($filterParts -join ";") `
    -map "[$previousLabel]" `
    -an `
    -c:v libx264 `
    -preset medium `
    -crf 18 `
    -pix_fmt yuv420p `
    -movflags +faststart `
    $silentVideo
  if ($LASTEXITCODE -ne 0) {
    throw "Transition render failed for $($format.Name)."
  }

  $outputPath = Join-Path $promoDir $format.Output
  & $ffmpeg -y `
    -i $silentVideo `
    -i $voicePath `
    -i $musicPath `
    -filter_complex "[1:a]adelay=350|350,aresample=48000,volume=1.10,apad=whole_dur=$durationText[voice];[2:a]volume=0.72[bed];[bed][voice]amix=inputs=2:duration=first:normalize=0,alimiter=limit=0.95[audio]" `
    -map 0:v `
    -map "[audio]" `
    -c:v copy `
    -c:a aac `
    -ar 48000 `
    -b:a 192k `
    -movflags +faststart `
    -t $durationText `
    $outputPath
  if ($LASTEXITCODE -ne 0) {
    throw "Final audio mix failed for $($format.Name)."
  }
}

Write-Host "YouTube video: $(Join-Path $promoDir 'msc-ambassador-youtube-1080p.mp4')"
Write-Host "Instagram Reel: $(Join-Path $promoDir 'msc-ambassador-instagram-reel.mp4')"
