$ErrorActionPreference = "Stop"

$promoDir = $PSScriptRoot
$repoRoot = Resolve-Path (Join-Path $promoDir "..\..")
$stockDir = Join-Path $promoDir "stock"
$visualDir = Join-Path $promoDir "visuals"
$buildRoot = Join-Path $promoDir "hinglish-build"
$sceneRoot = Join-Path $buildRoot "scenes"
$voicePath = Join-Path $promoDir "voiceover-hinglish.mp3"
$musicPath = Join-Path $promoDir "music-hinglish.wav"
$narrationPath = Join-Path $promoDir "narration-hinglish.txt"
$logoPath = Join-Path $repoRoot "ui\assets\msc-logo-512.png"
$fontFile = "C\:/Windows/Fonts/segoeuib.ttf"
$voice = "en-IN-PrabhatNeural"
$transitionDuration = 0.45

New-Item -ItemType Directory -Path $buildRoot, $sceneRoot -Force | Out-Null

$toolDir = Join-Path $env:TEMP "msc-promo-tools"
$ffmpeg = Join-Path $toolDir "node_modules\ffmpeg-static\ffmpeg.exe"
if (-not (Test-Path $ffmpeg)) {
  New-Item -ItemType Directory -Path $toolDir -Force | Out-Null
  & npm install --prefix $toolDir ffmpeg-static@5.2.0 --no-audit --no-fund
}
if (-not (Test-Path $ffmpeg)) {
  throw "Unable to install the local FFmpeg encoder."
}

$ttsDir = Join-Path $env:TEMP "msc-edge-tts"
if (-not (Test-Path (Join-Path $ttsDir "edge_tts"))) {
  New-Item -ItemType Directory -Path $ttsDir -Force | Out-Null
  & python -m pip install --target $ttsDir edge-tts
}
if (-not (Test-Path (Join-Path $ttsDir "edge_tts"))) {
  throw "Unable to install edge-tts for neural voiceover."
}

if (-not (Test-Path $narrationPath)) { throw "Missing narration file: $narrationPath" }
if (-not (Test-Path $logoPath)) { throw "Missing MSC logo: $logoPath" }

function Format-Decimal([double]$value) {
  return $value.ToString("0.###", [Globalization.CultureInfo]::InvariantCulture)
}

function Get-MediaDuration([string]$path) {
  $probe = cmd /c "`"$ffmpeg`" -hide_banner -i `"$path`" 2>&1"
  $probe = $probe | Out-String
  if ($probe -match "Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)") {
    return ([double]$matches[1] * 3600) + ([double]$matches[2] * 60) + [double]::Parse($matches[3], [Globalization.CultureInfo]::InvariantCulture)
  }
  throw "Unable to read media duration for $path"
}

function Escape-DrawText([string]$text) {
  return $text.Replace("\", "\\").Replace(":", "\:").Replace("'", "\'").Replace("%", "\%")
}

function Get-Layout([pscustomobject]$format) {
  if ($format.Name -eq "portrait") {
    return [pscustomobject]@{
      LogoSize = 82
      LogoX = 52
      LogoY = 58
      BoxX = 56
      BoxY = "ih-505"
      BoxW = "iw-112"
      BoxH = 360
      TitleY1 = "h-430"
      TitleY2 = "h-352"
      SubY = "h-252"
      TextX = 92
      TitleFont = 66
      SubFont = 38
      LegalFont = 21
      LegalY = 76
      LegalPad = 48
    }
  }

  return [pscustomobject]@{
    LogoSize = 72
    LogoX = 74
    LogoY = 54
    BoxX = 86
    BoxY = "ih-300"
    BoxW = "iw-172"
    BoxH = 220
    TitleY1 = "h-258"
    TitleY2 = "h-193"
    SubY = "h-121"
    TextX = 124
    TitleFont = 58
    SubFont = 31
    LegalFont = 24
    LegalY = 60
    LegalPad = 74
  }
}

function Add-SceneTextFilter([pscustomobject]$layout, [string]$title1, [string]$title2, [string]$subtitle) {
  $safeTitle1 = Escape-DrawText $title1
  $safeTitle2 = Escape-DrawText $title2
  $safeSubtitle = Escape-DrawText $subtitle
  $legal = Escape-DrawText "No guaranteed profit promise"

  $box = "drawbox=x=$($layout.BoxX):y=$($layout.BoxY):w=$($layout.BoxW):h=$($layout.BoxH):color=0x030711@0.90:t=fill"
  $line1 = "drawtext=fontfile='$fontFile':text='$safeTitle1':x=$($layout.TextX):y=$($layout.TitleY1):fontsize=$($layout.TitleFont):fontcolor=white:shadowcolor=black@0.65:shadowx=0:shadowy=2"
  $line2 = "drawtext=fontfile='$fontFile':text='$safeTitle2':x=$($layout.TextX):y=$($layout.TitleY2):fontsize=$($layout.TitleFont):fontcolor=0x45f0d2:shadowcolor=black@0.65:shadowx=0:shadowy=2"
  $sub = "drawtext=fontfile='$fontFile':text='$safeSubtitle':x=$($layout.TextX):y=$($layout.SubY):fontsize=$($layout.SubFont):fontcolor=0xdde7ff:shadowcolor=black@0.65:shadowx=0:shadowy=2"
  $legalText = "drawtext=fontfile='$fontFile':text='$legal':x=w-tw-$($layout.LegalPad):y=$($layout.LegalY):fontsize=$($layout.LegalFont):fontcolor=0xf0fff9@0.9:box=1:boxcolor=0x030711@0.48:boxborderw=16"

  return "$box,$line1,$line2,$sub,$legalText"
}

function Render-MotionScene(
  [pscustomobject]$format,
  [pscustomobject]$scene,
  [string]$outPath
) {
  $layout = Get-Layout $format
  $duration = Format-Decimal $scene.Duration
  $start = Format-Decimal $scene.Start
  $scaleCrop = "scale=$($format.Width):$($format.Height):force_original_aspect_ratio=increase,crop=$($format.Width):$($format.Height),fps=30"
  $textFilters = Add-SceneTextFilter $layout $scene.Title1 $scene.Title2 $scene.Subtitle
  $logoScale = "scale=$($layout.LogoSize):$($layout.LogoSize)"

  $filter = "[0:v]$scaleCrop,eq=contrast=1.04:saturation=1.10:brightness=-0.015,format=rgba[base];" +
    "[1:v]$logoScale[logo];" +
    "[base][logo]overlay=$($layout.LogoX):$($layout.LogoY),drawbox=x=0:y=0:w=iw:h=ih:color=0x01040b@0.08:t=fill,$textFilters,format=yuv420p[v]"

  & $ffmpeg -y `
    -stream_loop -1 `
    -ss $start `
    -t $duration `
    -i $scene.Source `
    -i $logoPath `
    -filter_complex $filter `
    -map "[v]" `
    -an `
    -r 30 `
    -c:v libx264 `
    -preset medium `
    -crf 18 `
    -pix_fmt yuv420p `
    $outPath

  if ($LASTEXITCODE -ne 0) {
    throw "Motion scene render failed: $($scene.Name) / $($format.Name)"
  }
}

function Render-ImageScene(
  [pscustomobject]$format,
  [pscustomobject]$scene,
  [string]$outPath
) {
  $layout = Get-Layout $format
  $duration = Format-Decimal $scene.Duration
  $textFilters = Add-SceneTextFilter $layout $scene.Title1 $scene.Title2 $scene.Subtitle
  $zoomStep = if ($scene.Name -in @("program", "rewards", "apply")) { "0.00005" } else { "0.00010" }
  $logoScale = "scale=$($layout.LogoSize):$($layout.LogoSize)"

  if ($format.Name -eq "portrait") {
    $fgWidth = 1000
    $fgHeight = 562
    $fgY = 266
    $panelX = [int](($format.Width - ($fgWidth + 20)) / 2)
    $panelY = $fgY - 10
    $panelW = $fgWidth + 20
    $panelH = $fgHeight + 20
    $portraitZoom = "zoompan=z='min(max(zoom,pzoom)+$zoomStep,1.022)':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':d=1:s=$($fgWidth)x$($fgHeight):fps=30"

    $filter = "[0:v]split=2[srcbg][srcfg];" +
      "[srcbg]scale=$($format.Width):$($format.Height):force_original_aspect_ratio=increase,crop=$($format.Width):$($format.Height),boxblur=18:2,eq=contrast=1.02:saturation=0.85:brightness=-0.14,format=rgba[bg];" +
      "[srcfg]scale=$($fgWidth):-2,$portraitZoom,format=rgba[fg];" +
      "[1:v]$logoScale[logo];" +
      "[bg]drawbox=x=${panelX}:y=${panelY}:w=${panelW}:h=${panelH}:color=0x030711@0.68:t=fill[panel];" +
      "[panel][fg]overlay=(W-w)/2:${fgY}[base];" +
      "[base][logo]overlay=$($layout.LogoX):$($layout.LogoY),$textFilters,format=yuv420p[v]"
  }
  else {
    $scaleCrop = "scale=$($format.Width):$($format.Height):force_original_aspect_ratio=increase,crop=$($format.Width):$($format.Height)"
    $zoom = "zoompan=z='min(max(zoom,pzoom)+$zoomStep,1.030)':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':d=1:s=$($format.Width)x$($format.Height):fps=30"

    $filter = "[0:v]$scaleCrop,$zoom,eq=contrast=1.03:saturation=1.08:brightness=-0.01,format=rgba[base];" +
      "[1:v]$logoScale[logo];" +
      "[base][logo]overlay=$($layout.LogoX):$($layout.LogoY),$textFilters,format=yuv420p[v]"
  }

  & $ffmpeg -y `
    -framerate 30 `
    -loop 1 `
    -i $scene.Source `
    -i $logoPath `
    -t $duration `
    -filter_complex $filter `
    -map "[v]" `
    -an `
    -r 30 `
    -c:v libx264 `
    -preset medium `
    -crf 18 `
    -pix_fmt yuv420p `
    $outPath

  if ($LASTEXITCODE -ne 0) {
    throw "Image scene render failed: $($scene.Name) / $($format.Name)"
  }
}

function Get-VideoDuration([array]$sceneList, [double]$transition) {
  $total = [double]$sceneList[0].Duration
  for ($i = 1; $i -lt $sceneList.Count; $i++) {
    $total += [double]$sceneList[$i].Duration - $transition
  }
  return $total
}

$env:PYTHONPATH = $ttsDir
& python -m edge_tts `
  --voice $voice `
  --rate "+10%" `
  --file $narrationPath `
  --write-media $voicePath
if ($LASTEXITCODE -ne 0) {
  throw "Hinglish neural voiceover generation failed."
}

$scenes = @(
  [pscustomobject]@{ Name = "hook"; Type = "motion"; Source = (Join-Path $stockDir "vlog.mp4"); Duration = 5.6; Start = 0.4; Title1 = "Creators future ko"; Title2 = "build karte hain"; Subtitle = "MSC Chain Ambassador Program" },
  [pscustomobject]@{ Name = "tech"; Type = "motion"; Source = (Join-Path $stockDir "laptop.mp4"); Duration = 5.3; Start = 1.1; Title1 = "Tech ko simple"; Title2 = "banao"; Subtitle = "Educators | Developers | Web3 pages" },
  [pscustomobject]@{ Name = "audience"; Type = "motion"; Source = (Join-Path $stockDir "team.mp4"); Duration = 5.1; Start = 0.7; Title1 = "Crypto | Gaming"; Title2 = "Tech creators"; Subtitle = "1K to 20K follower accounts" },
  [pscustomobject]@{ Name = "program"; Type = "image"; Source = (Join-Path $visualDir "portal-program.png"); Duration = 5.4; Start = 0.0; Title1 = "Bronze | Silver"; Title2 = "Gold levels"; Subtitle = "Badge | Content goals | Locked allocation" },
  [pscustomobject]@{ Name = "rewards"; Type = "image"; Source = (Join-Path $visualDir "portal-rewards.png"); Duration = 5.4; Start = 0.0; Title1 = "Rewards"; Title2 = "with vesting"; Subtitle = "MSC | Founder NFT | Referral points" },
  [pscustomobject]@{ Name = "community"; Type = "motion"; Source = (Join-Path $stockDir "presentation.mp4"); Duration = 5.1; Start = 1.2; Title1 = "Community"; Title2 = "engagement"; Subtitle = "AMA | Demos | Validator journey" },
  [pscustomobject]@{ Name = "apply"; Type = "image"; Source = (Join-Path $visualDir "portal-apply.png"); Duration = 5.2; Start = 0.0; Title1 = "Apply as"; Title2 = "Ambassador"; Subtitle = "Real followers | No fake traffic" },
  [pscustomobject]@{ Name = "outro"; Type = "motion"; Source = (Join-Path $stockDir "presentation.mp4"); Duration = 4.8; Start = 3.0; Title1 = "Educate. Engage."; Title2 = "Build MSC."; Subtitle = "Apply on MSC Ambassador Portal" }
)

foreach ($scene in $scenes) {
  if (-not (Test-Path $scene.Source)) {
    throw "Missing scene source: $($scene.Source)"
  }
}

$voiceDuration = Get-MediaDuration $voicePath
$baseDuration = Get-VideoDuration $scenes $transitionDuration
$targetDuration = $voiceDuration + 0.8
if ($baseDuration -lt $targetDuration) {
  $scenes[$scenes.Count - 1].Duration += ($targetDuration - $baseDuration)
}
$videoDuration = Get-VideoDuration $scenes $transitionDuration
$durationText = Format-Decimal $videoDuration
$fadeOutStart = Format-Decimal ([Math]::Max(0.1, $videoDuration - 1.6))

& $ffmpeg -y `
  -f lavfi -i "sine=frequency=62:sample_rate=48000:duration=$durationText" `
  -f lavfi -i "sine=frequency=124:sample_rate=48000:duration=$durationText" `
  -f lavfi -i "sine=frequency=248:sample_rate=48000:duration=$durationText" `
  -filter_complex "[0:a]volume=0.052,tremolo=f=2:d=0.68[bass];[1:a]volume=0.021,tremolo=f=0.34:d=0.38[mid];[2:a]volume=0.007,tremolo=f=0.16:d=0.25[high];[bass][mid][high]amix=inputs=3:duration=longest:normalize=0,afade=t=in:st=0:d=1.2,afade=t=out:st=$fadeOutStart`:d=1.6[music]" `
  -map "[music]" `
  -c:a pcm_s16le `
  $musicPath
if ($LASTEXITCODE -ne 0) {
  throw "Music generation failed."
}

$formats = @(
  [pscustomobject]@{ Name = "landscape"; Width = 1920; Height = 1080; Output = "msc-ambassador-youtube-hinglish-real.mp4" },
  [pscustomobject]@{ Name = "portrait"; Width = 1080; Height = 1920; Output = "msc-ambassador-instagram-hinglish-reel.mp4" }
)

foreach ($format in $formats) {
  $sceneDir = Join-Path $sceneRoot $format.Name
  New-Item -ItemType Directory -Path $sceneDir -Force | Out-Null

  foreach ($scene in $scenes) {
    $scenePath = Join-Path $sceneDir "$($scene.Name).mp4"
    if ($scene.Type -eq "image") {
      Render-ImageScene $format $scene $scenePath
    }
    else {
      Render-MotionScene $format $scene $scenePath
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
    $offsetText = Format-Decimal $offset
    $filterParts += "[$previousLabel][$index`:v]xfade=transition=fade:duration=$transitionDuration`:offset=$offsetText[$outputLabel]"
    $previousLabel = $outputLabel
    $runningDuration += [double]$scenes[$index].Duration - $transitionDuration
  }

  $silentVideo = Join-Path $buildRoot "$($format.Name)-silent.mp4"
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
    -filter_complex "[1:a]adelay=250|250,aresample=48000,volume=1.15,apad=whole_dur=$durationText[voice];[2:a]volume=0.55[bed];[bed][voice]amix=inputs=2:duration=first:normalize=0,alimiter=limit=0.95[audio]" `
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

  Write-Host "$($format.Name): $outputPath"
}

Write-Host "Voiceover: $voicePath"
Write-Host "Duration: $durationText seconds"
