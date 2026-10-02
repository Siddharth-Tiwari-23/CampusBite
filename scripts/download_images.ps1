$images = [ordered]@{
    'veg_momos.jpg' = 'https://upload.wikimedia.org/wikipedia/commons/d/dc/Momo_s.jpg'
    'paneer_momos.jpg' = 'https://images.unsplash.com/photo-1534422298391-e4f8c172dddb?w=800&auto=format&fit=crop&q=80'
    'hot_coffee.jpg' = 'https://images.unsplash.com/photo-1514432324607-a09d9b4aefdd?w=800&auto=format&fit=crop&q=80'
    'cold_coffee.jpg' = 'https://images.unsplash.com/photo-1517701550927-30cf4ba1dba5?w=800&auto=format&fit=crop&q=80'
    'chai_special.jpg' = 'https://images.unsplash.com/photo-1576092768241-dec231879fc3?w=800&auto=format&fit=crop&q=80'
    'paneer_paratha.jpg' = 'https://upload.wikimedia.org/wikipedia/commons/0/0b/Paratha_is_a_dough_fried_flatbread_native_to_India_and_Pakistan.jpg'
    'aloo_paratha.jpg' = 'https://upload.wikimedia.org/wikipedia/commons/f/fc/Alooparatha.jpg'
    'veg_samosa.jpg' = 'https://images.unsplash.com/photo-1601050690597-df0568f70950?w=800&auto=format&fit=crop&q=80'
    'french_fries.jpg' = 'https://images.unsplash.com/photo-1576107232684-1279f3908594?w=800&auto=format&fit=crop&q=80'
    'pasta_sada.jpg' = 'https://upload.wikimedia.org/wikipedia/commons/c/cd/Penne_Arrabbiata.jpg'
    'veg_pizza.jpg' = 'https://images.unsplash.com/photo-1513104890138-7c749659a591?w=800&auto=format&fit=crop&q=80'
    'spring_roll.jpg' = 'https://upload.wikimedia.org/wikipedia/commons/1/1e/Spring_Rolls_%283357696061%29.jpg'
}

$dir = 'd:\PROJECTS\CampusBite\frontend\public\images\menu'
if (-not (Test-Path $dir)) {
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
}

$headers = @{
    'User-Agent' = 'CampusBiteBot/1.0 (https://github.com/Siddharth-Tiwari-23/CampusBite; dev@campusbite.com) WindowsPowerShell'
}

foreach ($entry in $images.GetEnumerator()) {
    $target = Join-Path $dir $entry.Key
    Write-Host "Downloading $($entry.Key)..."
    Invoke-WebRequest -Uri $entry.Value -OutFile $target -Headers $headers
}

Write-Host "Downloaded images:"
Get-ChildItem -Path $dir
