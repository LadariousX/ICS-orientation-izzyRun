# PLAN.md

# Izzy the Islander

A browser-based arcade game for the ICS (Islander Cyber Society) table. Players compete for the highest score while a separate TV dashboard attracts attendees and displays the live leaderboard.
- go ahead and make differenft dirs for static, templates, assets all that crap
---

# Routes

## `/tv` – Event Display

This page is designed for a TV running in kiosk mode.

### Layout

Use a **3-column layout**.
bg color rgb (47, 109, 209)

| Area                     | Content                                                              |
|--------------------------|----------------------------------------------------------------------|
| Left column (rows 1 & 2) | **Future Islander Leaderboard** (spans both rows)   (js element)     |
| Top center               | ICS logo (img tag)                                                   |
| Bottom center            | ICS photo slideshow (simple image rotation, no transitions required) |
| right column             | jpg poster (img tag)                                                 |

the game will have send baack a player name and score to the server on game over. those will be stored in 
a file and the top X (5 for now) will be displayed on the leaderboard. it will need some way to auto-update that.


## `/game` – Play the Game

### landing page
- horizontal orientation designed for mobile devices.
- vertically scrolling upward (looping) png background
- pop up requesting name with start button 
- when user presses start button, popup disappears by shrinking to a point.
- scrolling background stops looping, speeds up its vertical scroll upward to uncover the game.

### gameplay
similar to the dino game but features my collage mascot, Izzy. the game will run horizontal on mobile phones, best if 
its made in JavaScript. png sprites will make up the game including the background wich will be a looping image. The 
wave and pelican in this game are similar to the cactus and taradactal from chrome dino run, they will not have animations. 
Izzy will cycle between running sprites then his jump and duck sprites when the player does those.

the game will have up and down arrows on the right side of the screen so no need to program swipe jestures. 

collision with the obstical sprites will trigger a game over popup. with current rank on leaderboard a message if they set a record. (just save those values serverside in a csv in /db)

probably best if the PNG sprites were in there iwn dirs in a sprite dir. let me know if it would be easyer to load 
sprites on individual files or if there should be sprite sheets tiled. 