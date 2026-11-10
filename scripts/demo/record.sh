#!/bin/bash
# Records the demos in docs/media: a video and a screenshot per skin for the
# website, and demo.gif (ending in a tour of all skins) for the README.
#
# Needs vhs, ffmpeg, ImageMagick, python3 with numpy and the JetBrainsMono
# Nerd Font (or set CASSETTY_DEMO_FONT).
#
# usage: scripts/demo/record.sh [skin...]   (default: all skins and the GIF)
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
media=$repo/docs/media
work=${CASSETTY_DEMO_DIR:-${TMPDIR:-/tmp}/cassetty-demo}
font=${CASSETTY_DEMO_FONT:-JetBrainsMono Nerd Font Mono}

skins=("$@")
gif=0
if [ ${#skins[@]} -eq 0 ]; then
	skins=(sunbleached silverface cyberdeck classic amber vfd synthwave)
	gif=1
fi

mkdir -p "$work/cfg" "$work/state" "$work/out"
(cd "$repo" && go build -o "$work/cassetty" .)
[ -d "$work/Music" ] || python3 "$here/library.py" "$work/Music"
cd "$work"

# tape <skin> <site|readme> writes the vhs script for one recording.
tape() {
	local skin=$1 kind=$2
	if [ "$kind" = site ]; then
		echo "Output out/$skin.mp4"
	else
		echo "Output out/demo.gif"
	fi
	cat <<EOF
Set Shell "bash"
Set FontFamily "$font"
Set FontSize 15
Set Width 1200
Set Height 620
Set Padding 0
Set Framerate 20
Set TypingSpeed 0
Env COLORTERM "truecolor"
Env XDG_CONFIG_HOME "$work/cfg"
Env XDG_STATE_HOME "$work/state"
Hide
Type "clear && exec ./cassetty -silent -theme $skin Music"
Enter
Sleep 1.5s
Show
Sleep 1.2s
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 700ms
Enter
Sleep 700ms
Enter
Sleep 900ms
Enter
Sleep 3.5s
Type "]"
Sleep 400ms
Type "]"
Sleep 2.5s
Space
Sleep 1.6s
Space
Sleep 1.2s
Type "n"
Sleep 2.5s
EOF
	[ "$kind" = site ] && echo "Screenshot out/$skin.png"
	cat <<EOF
Sleep 1s
Tab
Sleep 3.2s
Tab
Sleep 1.2s
EOF
	if [ "$kind" = readme ]; then
		for _ in 1 2 3 4 5 6 7; do
			printf 'Type "t"\nSleep 1.3s\n'
		done
	fi
}

for skin in "${skins[@]}"; do
	echo "recording $skin"
	tape "$skin" site >out/site.tape
	vhs out/site.tape >/dev/null
	cp "out/$skin.mp4" "$media/"
	magick "out/$skin.png" -strip -colors 256 "PNG8:$media/$skin.png"
done

if [ "$gif" = 1 ]; then
	echo "recording demo.gif"
	tape sunbleached readme >out/readme.tape
	vhs out/readme.tape >/dev/null
	ffmpeg -loglevel error -y -i out/demo.gif -vf "fps=12,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" "$media/demo.gif"
fi
