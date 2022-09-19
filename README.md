# Video chat privacy
Automatic Background Removal for Improved Video Chat Privacy of Galene

<p align="center">
  <img src="/images/logo.png" width="350" title="Sensifai" alt="Sensifai logo">
</p>

## Introduction
Making video calls can be very invasive to privacy: the camera does not only capture the face and posture of the person talking, but will in fact capture the entire environment in glorious high definition - from the books in your bookshelf to family members or laundry rack behind you. This information is of no interest to the other end, but with a camera you have little choice: once you slide open the camera cover, it takes everything within the field of view and broadcasts it to the other side. This project aims to use advanced AI technology to edit the video feed in real-time, and apply various privacy enhancements such as removal of backgrounds.
One of the things people enjoy the most about the internet, is that it enables them to talk to others remotely almost without limit. Internet allows anyone to keep closely connected with friends and family, and help their kids solve a math problem while they are at work. People collaborate with their colleagues from the couch of their living room, the cafe where they enjoy lunch or on their cell phone on the bus to the gym. Businesses can easily service their customers where this is most convenient to them, without having to travel themselves. This is so convenient, that some businesses have already moved entirely online. Internet communication has become the nerve center of whole neighborhoods, where people watch over the possessions of their neighbors while these are away for work or leisure.
However, users have a hard time to understand how privacy is impacted if they use the wrong technology. Because internet works almost everywhere, the natural privacy protection of the walls of a house, a school or an office is gone. For example, when you make a video call, your webcam or phone camera captures a lot more than just you talking, for example the people around you, the books on your shelf or the street outside. A lot of this information can be used to uniquely identify you or to find your location, which you may not always be aware of. Because high definition cameras are embedded in more and more devices everywhere around us, we need more control over what these digital eyes actually record about us.
Instead of only being able to switch your webcam on or off, this project will develop technology that lets you remove or anonymize the background client-side while you are actually making a video call. It will integrate this technology in a video conferencing app, giving users another tools to protect their visual privacy and fight back against all sorts of sophisticated tracking schemes.

## Background Remover Overal Architecture
In this section, we present our approach toward developing a video chat system equipped with live background removal based on artificial intelligence. Figure 1 shows the general block-diagram of the background removal system for an online video chat platform. As it can be seen in this figure, we develop an AI based image processing system on the client side using Tensorflow.js which removes the background of video-chat in real-time. The video stream is then sent to the TURN server using webRTC and finally it is delivered to the other side of the conversation, who also goes through the same process in real-time. 

<p align="left">
  <img src="/images/arch.jpg" title="Architecture" alt="Architecture">
  Fig 1: The general block-diagram of the background removal system for an online video chat platform.
</p>

## Remove Background at Client-Side using TensorFlow.js
TensorFlow.js is a JavaScript version of Tensorflow. is a very popular Deep-learning tool developed by Google. TensorFlow.js brings the power of TensorFlow to JavaScript where it can be used in node.js and the browser. Although TensorFlow.js includes many pre-built models for computer vision, we have developed our own real-time human segmentation model to remove the background.
Tensorflow.js is also WebRTC friendly and includes helper functions that automatically extract images from video feeds. For example, some functions, like tf.data.webcam (webcamElement) will even call getUserMedia for you. 
Our Background Removing software segments the prominent humans in the scene. It can run in real-time on both smartphones and laptops. The intended use cases include selfie effects and video conferencing, where the person is close (< 2m) to the camera.

### A- Person/pose Detection Model (BlazePose Detector)
The detector is inspired by lightweight BlazeFace model for a person detector. It explicitly predicts two additional virtual keypoints that firmly describe the human body center, rotation and scale as a circle. We predict the midpoint of a person’s hips, the radius of a circle circumscribing the whole person, and the incline angle of the line connecting the shoulder and hip midpoints.

### B- Person Segmentation Model
Background segmentation mask on the whole body from RGB video frames utilizing our BlazePose research that also powers the ML Kit Pose Detection API. Current state-of-the-art approaches rely primarily on powerful desktop environments for inference, whereas our method achieves real-time performance on most modern mobile phones, desktops/laptops, in python and even on the web.

### C- How it works?
#### a)	Input video
Read frames from a video file, webcam, or WebRTC stream and send them as an image to the segmentation function for the process.
```javascript
const camera = new Camera(video, {
  onFrame: async () => {
    await segmentation.send({image: video});
  },
  width: 1280, height: 720
});
camera.start();
```
<p align="left">
  <img src="/images/1.jpg" title="Input frame" alt="Input frame">
  Input frame
</p>

#### b)	AI Script in JS
Run the deep learning model in the pipeline to get a frame, segment foreground, and background, and return output to the result function.

```javascript
function result(results) {
. . . 
}

const segmentation = new Segmentation({locateFile: (file) => {
  return `${path/to/file}`;
}});
segmentation.setOptions({
  modelSelection: 1,
});
segmentation.onResults(result);
```
<p align="left">
  <img src="/images/2.png" title="segmentation mask" alt="segmentation mask">
  segmentation mask
</p>

#### c)	Output in HTML
In the final step, result method get the AI model’s output and show it in a html canvas tag in the browser.

```html
<body>
    <canvas class="output"></canvas>
</body>
```

<p align="left">
  <img src="/images/3.png" title="Remove background" alt="sRemove background">
  Remove background
</p>
<p align="left">
  <img src="/images/4.jpg" title="New image background" alt="New image background">
  New image background
</p>

### Performance
Infer time is evaluated on **Full-HD** resolution using **Intel Corei5-6500 3.20GHz** CPU.
Language      | SModel Complexity | FPS
------------- | ----------------- | ---
Python  | Lite | 39.39
Python  | Full | 30.04
Python  | Heavy | 8.20
JavaScript  | Lite | 30
JavaScript  | Full | 29.5
JavaScript  | Heavy | 29.5
C++  |  | 
Android  |  | 