# Architecture
Waveguide under the hood is a interface layer for working with WebRTC.


# Protocols
Protocols are publicly available implementations of a specific technology usable for other projects.

Examples of protocols include: RTMP, SRT, FTL, WebRTC, HLS, etc

# Inputs
Inputs are Waveguide specific implementations of a specific protocol, either one maintained by us, or one maintained by 3rd parties. Inputs handle facilitation between Protocols and the Control.

Examples of inputs include: FTL, RTMP, etc

# Control
The Control is the middle piece between the ingests and waveguide. Control is responsible for authenticating new streams against a Service, managing a known state of those streams, and managing tracks.

# Outputs
Outputs are Waveguide specific implementations of a specific protocol, the same as inputs. They handle accepting incoming viewers, and fetching the media packets needed to serve up the stream to the user. A output can be as simple as a WebRTC video stream, or an output could save entire streams to a disk for later consumption. A user of an output can also be another Waveguide server that relays the video over WHEP.

Examples of outputs include: WHEP (WebRTC), HLS, etc

# Orchestrator 
The Orchestrator has been simplified and is now responsible for load balancing WHIP and WHEP users to servers that meet their latency or load needs. The Control only tells the Orchestrator where a stream is, and when the stream is done. 

## Improved design based on simple HTTP based Orchestrator with WHEP (and WHIP!)
```mermaid
sequenceDiagram
    title Specific Control Path

    participant Input
    participant LocalControl
    participant LocalPeerConnection
    participant Orchestrator
    participant RemotePeerConnection
    participant RemoteControl
    participant Output

    %% Note over Input: Does not interact with <br> WebRTC directly
    %% Note over Output: Does not interact with <br> WebRTC directly

    Input ->> LocalControl: AddChannel(1234)
    LocalControl ->> Orchestrator: AddChannel(1234)
    %% LocalControl ->> LocalPeerConnection: Create Peer Connection
    %% Note over Input, LocalControl: Peer Connection is unused <br>until a peer connects
    Input ->> LocalControl: AddTrack(video / audio)
    %% LocalControl ->> LocalPeerConnection: Add Track
    Note over LocalControl: Keeps track of tracks <br>until needed by PC

    loop Every viewer connection
        Note over Output: Viewer watches 1234
        Output ->>+ RemoteControl: WatchChannel(1234)
        RemoteControl ->> Orchestrator: /whep/watch/1234
        Note over Orchestrator: Figures out where the stream is <br>then redirects to LocalPeerConnection whep
        Note over Orchestrator: Follows WHEP spec closely
        RemoteControl ->>- LocalPeerConnection: /whep/watch/1234
        LocalPeerConnection ->> RemotePeerConnection: SDP Offer
        RemotePeerConnection ->> LocalPeerConnection: SDP Answer

        %% RemotePeerConnection ->> RemoteControl: OnTrack
        %% RemoteControl ->> Output: Tracks
        %% Note over RemoteControl, Output: Tells Output what formats <br> to expect as input
        loop WebRTC RTP
            LocalPeerConnection -> RemotePeerConnection: WebRTC Stuff
        end
    end
```

### For WHIP (assuming WebRTC OBS)
```mermaid
sequenceDiagram
    title Specific Control Path

    participant Streamer
    participant Input
    participant LocalControl
    participant LocalPeerConnection
    participant Orchestrator

    loop Every streamer connection
        Note over Input: Streamer streams to Glimesh
        Streamer ->> Orchestrator: /whip/stream/1234-foobara
        Note over Orchestrator: Figures out the best ingest <br>server for the stream
        Orchestrator ->> Streamer: Redirect to Input/whip/stream/1234-foobara
        Streamer ->> Input: /whip/stream/1234-foobara
        
        
        Streamer ->> LocalPeerConnection: SDP Offer
        LocalPeerConnection ->> Streamer: SDP Answer

        %% RemotePeerConnection ->> RemoteControl: OnTrack
        %% RemoteControl ->> Output: Tracks
        %% Note over RemoteControl, Output: Tells Output what formats <br> to expect as input
        loop WebRTC RTP
            Streamer -> LocalPeerConnection: WebRTC Stuff
        end
    end
```






## Historical Architecture Ideas
```mermaid
sequenceDiagram
    title FTL Streamer Simple Input / Output

    participant Streamer
    participant FTL Input
    participant Glimesh.tv
    participant WHEP Output
    participant Viewer

    Streamer ->> FTL Input: FTL Handshake
    FTL Input ->> Glimesh.tv: Verify credentials
    Glimesh.tv ->> FTL Input: Stream ID
    Streamer ->>+ FTL Input: Media Packets
    FTL Input ->>- WHEP Output: Media Packets

    loop
        Streamer ->> FTL Input: FTL Ping
        FTL Input ->> Streamer: FTL Pong
    end

    Note right of WHEP Output: Anytime during stream

    Viewer ->>+ WHEP Output: WHEP Handshake
    WHEP Output ->>- Viewer: WHEP Handshake

    WHEP Output ->> Viewer: Media Packets
```

```mermaid
sequenceDiagram
    title Generic Input / Output with Control & Orchestrator

    participant Streamer
    participant Input
    participant Control
    participant Service
    participant Orchestrator
    participant Output
    participant Viewer

    Streamer ->>+ Input: Start Stream
    Input ->> Control: Authenticate 
    Control ->> Service: Authenticate
    Service ->> Control: Approved
    Note over Control: Add Stream to State
    Control ->> Input: Approved
    Note over Input: Assign port, etc
    Input ->>- Streamer: Start Media Packets

    Streamer ->> Input: Media Packets
    Note over Control: What does Control provide the <br> Input for media packets?
    Input ->> Control: Write Media Packets?

    loop Every 5 seconds
        Input ->> Control: Stream Metadata
        Control ->> Service: Stream Metadata
    end

    Control ->> Orchestrator: Notify Stream Existance 
    
    par Async Viewer Process
        Note left of Viewer: Anytime during stream

        Viewer ->>+ Output: Watch Stream
        Output ->> Control: Watch Stream
        Control ->> Orchestrator: Where is stream?
        Note over Orchestrator: Stream could be local or remote
        Control ->> Output: Media Packets
        Output ->>- Viewer: Media Packets
    end
```

## Architecture Choices
### How do Media Packets make it from an Input to an Output on a local host?
Can we just have a list of writers that the Control should be responsible to writing to? Then they could be net.Conn or io.Writer for local.
```go
buf := new(bytes.Buffer)
conn, _ := net.Dial("udp", "127.0.0.1:1234")

manyWriters := [3]io.Writer{
    buf,
    os.Stdout,
    conn,
}

for _, writer := range manyWriters {
    writer.Write([]byte{1, 2, 3})
}
```

OR 

We use WebRTC all the way through, with the Inputs and Outputs speaking to each other via WebRTC.
```mermaid
sequenceDiagram
    title Specific Control Path

    participant Input
    participant LocalControl
    participant LocalPeerConnection
    participant Orchestrator
    participant RemotePeerConnection
    participant RemoteControl
    participant Output

    Note over Input: Does not interact with <br> WebRTC directly

    Input ->> LocalControl: AddStream(1234)
    LocalControl ->> LocalPeerConnection: Create Peer Connection
    Note over Input, LocalControl: Peer Connection is unused <br>until a peer connects
    Input ->> LocalControl: AddTrack(video / audio)
    LocalControl ->> LocalPeerConnection: Add Track

    Note over Output: Viewer watches 1234
    Output ->> RemoteControl: WatchChannel(1234)
    RemoteControl ->> Orchestrator: Where is 1234?
    Orchestrator ->> RemoteControl: 1234 is at LocalPeerConnection

    RemoteControl ->> RemotePeerConnection: Connect to LocalPeerConnection

    RemotePeerConnection ->> LocalPeerConnection: WHEP /watch/1234
    LocalPeerConnection ->> RemotePeerConnection: SDP Offer
    RemotePeerConnection ->> LocalPeerConnection: SDP Answer

    RemotePeerConnection ->> RemoteControl: OnTrack
    RemoteControl ->> Output: Tracks
    Note over RemoteControl, Output: Tells Output what formats <br> to expect as input
    

    loop On RTP
        Input ->> LocalControl: WriteRTP(1234, packet)
        LocalControl ->> LocalPeerConnection: Write to Track
        Note over LocalPeerConnection, RemotePeerConnection: Async and many client capable
        LocalPeerConnection ->> RemotePeerConnection: Write to Track
        Note over RemotePeerConnection, RemoteControl: OnRTP allows for multiple Output <br> of the same content
        RemotePeerConnection ->> RemoteControl: OnRTP
        RemoteControl ->> Output: Send Packets
    end
```
