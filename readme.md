# Waveguide
Waveguide is a _revolutionary_ live streaming facilitation server. Built around WebRTC it helps not only ingest video from many sources, but route that video around to various peers. Waveguide can be configured for a simple setup, or a out-of-the-box video streaming empire.

Waveguide is a very early work in progress.

## Todo

- Setup Inputs to send control regular metadata info
- Setup control to aggregate that metadata into a Service UpdateStreamMetadata call
- Setup Inputs to store some video bytes in a frame buffer 
- Setup control to take a video frame buffer and convert it into a Service SendJpegPreviewImage call