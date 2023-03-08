# Waveguide
Waveguide is a _revolutionary_ live streaming facilitation server. Built around WebRTC it helps not only ingest video from many sources, but route that video around to various peers. Waveguide can be configured for a simple setup, or a out-of-the-box video streaming empire.

Waveguide is a very early work in progress.

### macOS Development
```
brew install opusfile fdk-aac
```

### Ubuntu / Linux Development
```
apt install -y pkg-config build-essential libopusfile-dev libfdk-aac-dev libavutil-dev libavcodec-dev libswscale-dev
```

## Building
A simple `go build` will build Waveguide assuming the above system dependencies are installed.

## Configuration
A sample configuration is provided in `config.toml.example`, you can copy that file to `config.toml` to have an out of the box streaming experience.

### Testing Waveguide Locally
Using the example `config.toml.example` you'll get a Waveguide server with RTMP and FTL inputs, hooked into a dummy orchestrator and service. By default the dummy service stream key format is `ChannelID-Sha256Hash`, so for a ChannelID of `1234` your resulting stream key would be `1234-03ac674216f3e15c761ee1a5e255f067953623c8b388b4459e13f978d7c846f4`.

You can export a stream key for testing using: 
```
export RTMP_URL=rtmp://localhost/live/1234-03ac674216f3e15c761ee1a5e255f067953623c8b388b4459e13f978d7c846f4
```
and then use ffmpeg to produce a RTMP stream:
```
ffmpeg -re -f lavfi \
    -i "testsrc=size=1280x720:rate=60[out0];sine=frequency=1000:sample_rate=48000[out1]" \
    -vf "[in]drawtext=fontsize=96: box=1: boxcolor=black@0.75: boxborderw=5: fontcolor=white: x=(w-text_w)/2: y=((h-text_h)/2)+((h-text_h)/-2): text='Hello from FFmpeg', drawtext=fontsize=96: box=1: boxcolor=black@0.75: boxborderw=5: fontcolor=white: x=(w-text_w)/2: y=((h-text_h)/2)+((h-text_h)/2): text='%{gmtime\:%H\\\\\:%M\\\\\:%S} UTC'[out]" \
    -nal-hrd cbr \
    -metadata:s:v encoder=test \
    -vcodec libx264 \
    -acodec aac \
    -preset veryfast \
    -profile:v baseline \
    -tune zerolatency \
    -bf 0 \
    -g 0 \
    -b:v 6320k \
    -b:a 160k \
    -ac 2 \
    -ar 48000 \
    -minrate 6320k \
    -maxrate 6320k \
    -bufsize 6320k \
    -muxrate 6320k \
    -r 60 \
    -pix_fmt yuv420p \
    -color_range 1 -colorspace 1 -color_primaries 1 -color_trc 1 \
    -flags:v +global_header \
    -bsf:v dump_extra \
    -x264-params "nal-hrd=cbr:min-keyint=2:keyint=2:scenecut=0:bframes=0" \
    -f flv "$RTMP_URL"
```

Once ffmpeg is sending bits to Waveguide, you can open your browser to `http://localhost:8091/stream/1234` to view your stream. You can replace 1234 with any Channel ID you are testing with.